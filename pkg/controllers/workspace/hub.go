package workspace

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"

	"github.com/skeeey/kcp-integration/pkg/controllers/workspace/csr"
	"github.com/skeeey/kcp-integration/pkg/helpers"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
)

//go:embed manifests
var manifestFiles embed.FS

// var workspaceFiles = []string{
// 	"manifests/workspace/kube-system-namespace.yaml",
// 	"manifests/workspace/extension-apiserver-authentication-cm.yaml",
// }

var hubFiles = []string{
	"manifests/hub/cluster-manager-namespace.yaml",
	"manifests/hub/external-hub-kubeconfig-secret.yaml",
	"manifests/hub/cluster-manager-registration-webhook-service.yaml",
	"manifests/hub/cluster-manager-work-webhook-service.yaml",
	"manifests/hub/cluster-manager.yaml",
}

type hubWorkspaceController struct {
	orgWorkspaceName    string
	certFile            string
	keyFile             string
	kcpRestConfig       *rest.Config
	hubKubeClient       kubernetes.Interface
	clusterMangerClient operatorv1client.ClusterManagerInterface
	workspaceLister     cache.GenericLister
	cache               resourceapply.ResourceCache
	controllers         map[string]context.CancelFunc
	eventRecorder       events.Recorder
}

func NewHubWorkspaceController(
	orgWorkspaceName string,
	certFile, keyFile string,
	kcpRestConfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	clusterMangerClient operatorv1client.ClusterManagerInterface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder) factory.Controller {
	ctrl := &hubWorkspaceController{
		orgWorkspaceName:    orgWorkspaceName,
		certFile:            certFile,
		keyFile:             keyFile,
		kcpRestConfig:       kcpRestConfig,
		hubKubeClient:       hubKubeClient,
		clusterMangerClient: clusterMangerClient,
		workspaceLister:     workspaceInformer.Lister(),
		cache:               resourceapply.NewResourceCache(),
		controllers:         map[string]context.CancelFunc{},
		eventRecorder:       recorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			workspaceInformer.Informer(),
		).
		WithSync(ctrl.sync).
		ToController(fmt.Sprintf("%s-workspaces-controller", orgWorkspaceName), recorder)
}

func (c *hubWorkspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil negotiation workspace %s", workspaceName)

	workspace, err := c.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	//TODO add finalizer on workspace to handle workspace deletation

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return nil
	}

	workspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	workspaceConfig.Host = helpers.GetWorkspaceURL(workspace)

	// prepare kube-system/extension-apiserver-authentication configmap for manager cluster webhooks in the workspace
	// TODO may ask kcp to support
	// if err := ctrl.prepareAuthConfigMap(ctx, syncCtx, workspaceConfig); err != nil {
	// 	return err
	// }

	// prepare cluster manager on hub
	if err := c.prepareClusterManager(ctx, syncCtx, workspaceName, workspaceConfig); err != nil {
		return err
	}

	return c.startHubControllers(ctx, workspaceName, workspaceConfig)
}

// func (ctrl *ocmHubWorkspaceController) prepareAuthConfigMap(ctx context.Context, syncCtx factory.SyncContext, restConfig *rest.Config) error {
// 	workspaceKubeClient, err := kubernetes.NewForConfig(restConfig)
// 	if err != nil {
// 		return err
// 	}

// 	config := struct {
// 		CABundle    string
// 		ReqCABundle string
// 	}{
// 		CABundle:    helpers.Indent(4, restConfig.CAData),
// 		ReqCABundle: helpers.Indent(4, restConfig.CAData),
// 	}

// 	return helpers.ApplyObjects(ctx, workspaceKubeClient, nil, syncCtx.Recorder(), manifestFiles, config, workspaceFiles...)
// }

func (c *hubWorkspaceController) prepareClusterManager(ctx context.Context,
	syncCtx factory.SyncContext,
	workspaceName string,
	restConfig *rest.Config) error {
	kubeConfigData, err := clientcmd.Write(buildKubeconfig(restConfig))
	if err != nil {
		return err
	}

	config := struct {
		ClusterManagerName string
		KubeConfig         string
		Org                string
		Workspace          string
	}{
		ClusterManagerName: fmt.Sprintf("kcp-%s-%s-cluster-manager", c.orgWorkspaceName, workspaceName),
		KubeConfig:         base64.StdEncoding.EncodeToString(kubeConfigData),
		Org:                c.orgWorkspaceName,
		Workspace:          workspaceName,
	}

	return helpers.ApplyObjects(
		ctx,
		c.hubKubeClient,
		c.clusterMangerClient,
		syncCtx.Recorder(),
		manifestFiles,
		config,
		hubFiles...,
	)
}

func (c *hubWorkspaceController) startHubControllers(ctx context.Context,
	workspaceName string,
	restConfig *rest.Config) error {
	if c.controllers[workspaceName] != nil {
		return nil
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)
	c.controllers[workspaceName] = cancel

	kubeInfomer := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)

	csrSigningController := csr.NewCSRSigningController(
		workspaceName,
		c.certFile,
		c.keyFile,
		kubeClient,
		kubeInfomer.Certificates().V1().CertificateSigningRequests(),
		c.eventRecorder,
	)

	go kubeInfomer.Start(workspaceCtx.Done())

	go csrSigningController.Run(workspaceCtx, 1)

	return nil
}

func buildKubeconfig(restConfig *rest.Config) clientcmdapi.Config {
	return clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   restConfig.Host,
			CertificateAuthorityData: restConfig.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			Token: restConfig.BearerToken,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}
}
