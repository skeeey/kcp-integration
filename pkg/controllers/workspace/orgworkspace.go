package workspace

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"

	"github.com/skeeey/kcp-integration/pkg/helpers"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type orgWorkspaceController struct {
	certFile            string
	keyFile             string
	kcpRestConfig       *rest.Config
	hubKubeClient       kubernetes.Interface
	clusterMangerClient operatorv1client.ClusterManagerInterface
	workspaceLister     cache.GenericLister
	workspaces          map[string]context.CancelFunc
	eventRecorder       events.Recorder
}

func NewOrgWorkspaceController(
	certFile, keyFile string,
	kcpRestConfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	clusterMangerClient operatorv1client.ClusterManagerInterface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder,
) factory.Controller {
	ctrl := &orgWorkspaceController{
		certFile:            certFile,
		keyFile:             keyFile,
		kcpRestConfig:       kcpRestConfig,
		hubKubeClient:       hubKubeClient,
		clusterMangerClient: clusterMangerClient,
		workspaceLister:     workspaceInformer.Lister(),
		workspaces:          map[string]context.CancelFunc{},
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
		ToController("all-org-workspaces-controller", recorder)
}

func (c *orgWorkspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil workspace %s", workspaceName)

	workspace, err := c.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// only handle organization workspace currently
	// TODO do we support to create the hub workspace on the kcp root??
	if helpers.GetWorkspaceType(workspace) != "organization" {
		klog.Infof("ignore the workspace %s", workspaceName)
		return nil
	}

	//TODO add finalizer on this workspace to handle workspace deletation

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return nil
	}

	// start a controller for this workspace
	return c.startOCMHubWorkspaceController(ctx, workspaceName, helpers.GetWorkspaceURL(workspace))
}

func (c *orgWorkspaceController) startOCMHubWorkspaceController(
	ctx context.Context,
	workspaceName, workspaceURL string) error {
	if c.workspaces[workspaceName] != nil {
		return nil
	}

	hubWorkspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	hubWorkspaceConfig.Host = workspaceURL

	dynamicClient, err := dynamic.NewForConfig(hubWorkspaceConfig)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)
	c.workspaces[workspaceName] = cancel

	workspaceDynamicInformer := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		dynamicClient,
		10*time.Minute,
		metav1.NamespaceAll,
		func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = "kcp-integration.open-cluster-management.io/hub=true"
		},
	)

	hubWorkspaceController := NewHubWorkspaceController(
		workspaceName,
		c.certFile,
		c.keyFile,
		c.kcpRestConfig,
		c.hubKubeClient,
		c.clusterMangerClient,
		workspaceDynamicInformer.ForResource(helpers.ClusterWorkspaceGVR),
		c.eventRecorder,
	)

	go workspaceDynamicInformer.Start(workspaceCtx.Done())

	go hubWorkspaceController.Run(workspaceCtx, 1)

	return nil
}
