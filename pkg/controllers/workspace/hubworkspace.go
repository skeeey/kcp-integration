package workspace

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
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

//var manifestFiles embed.FS

var hubFiles = []string{
	"manifests/hub/cluster-manager-namespace.yaml",
	"manifests/hub/external-hub-kubeconfig-secret.yaml",
	// TODO services are only for prototype
	"manifests/hub/cluster-manager-registration-webhook-service.yaml",
	"manifests/hub/cluster-manager-work-webhook-service.yaml",
	"manifests/hub/cluster-manager.yaml",
}

type hubWorkspaceController struct {
	orgWorkspaceName            string
	certFile                    string
	keyFile                     string
	kcpRestConfig               *rest.Config
	hubKubeClient               kubernetes.Interface
	clusterMangerClient         operatorv1client.ClusterManagerInterface
	workspaceLister             cache.GenericLister
	cache                       resourceapply.ResourceCache
	controllers                 map[string]context.CancelFunc
	registrationWebhookNodePort int //TODO: just for prototype
	workWebhookNodePort         int //TODO: just for prototype
	eventRecorder               events.Recorder
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
		orgWorkspaceName:            orgWorkspaceName,
		certFile:                    certFile,
		keyFile:                     keyFile,
		kcpRestConfig:               kcpRestConfig,
		hubKubeClient:               hubKubeClient,
		clusterMangerClient:         clusterMangerClient,
		workspaceLister:             workspaceInformer.Lister(),
		cache:                       resourceapply.NewResourceCache(),
		controllers:                 map[string]context.CancelFunc{},
		registrationWebhookNodePort: 30442,
		workWebhookNodePort:         30452,
		eventRecorder:               recorder,
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

	// prepare cluster manager on hub
	if err := c.prepareClusterManager(ctx, workspaceName, workspaceConfig); err != nil {
		return err
	}

	// wait the cluster manager is deployed
	if !helpers.IsWorkspaceStatusConditionTrue(workspace, "ClusterManagerApplied") {
		return nil
	}

	return c.startHubControllers(ctx, workspaceName, workspaceConfig)
}

func (c *hubWorkspaceController) prepareClusterManager(ctx context.Context,
	workspaceName string,
	restConfig *rest.Config) error {
	kubeConfigData, err := clientcmd.Write(buildKubeconfig(restConfig))
	if err != nil {
		return err
	}

	c.registrationWebhookNodePort = c.registrationWebhookNodePort + 1
	c.workWebhookNodePort = c.workWebhookNodePort + 1

	config := struct {
		ClusterManagerName          string
		KubeConfig                  string
		Org                         string
		Workspace                   string
		RegistrationWebhookNodePort int
		WorkWebhookNodePort         int
	}{
		ClusterManagerName:          fmt.Sprintf("kcp-%s-%s-cluster-manager", c.orgWorkspaceName, workspaceName),
		KubeConfig:                  base64.StdEncoding.EncodeToString(kubeConfigData),
		Org:                         c.orgWorkspaceName,
		Workspace:                   workspaceName,
		RegistrationWebhookNodePort: c.registrationWebhookNodePort,
		WorkWebhookNodePort:         c.workWebhookNodePort,
	}

	return helpers.ApplyObjects(
		ctx,
		c.hubKubeClient,
		nil,
		c.eventRecorder,
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

	clusterClient, err := clusterclient.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)
	c.controllers[workspaceName] = cancel

	kubeInfomer := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	csrSigningController := csr.NewCSRSigningController(
		workspaceName,
		c.certFile,
		c.keyFile,
		kubeClient,
		kubeInfomer.Certificates().V1().CertificateSigningRequests(),
		c.eventRecorder,
	)

	clusterController := NewClusterController(
		c.orgWorkspaceName,
		workspaceName,
		c.kcpRestConfig,
		clusterInformers.Cluster().V1().ManagedClusters(),
		c.eventRecorder,
	)

	go kubeInfomer.Start(workspaceCtx.Done())
	go clusterInformers.Start(workspaceCtx.Done())

	go csrSigningController.Run(workspaceCtx, 1)
	go clusterController.Run(workspaceCtx, 1)

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
