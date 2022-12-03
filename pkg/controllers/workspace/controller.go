package workspace

import (
	"context"
	"embed"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"open-cluster-management.io/registration/pkg/hub"

	"github.com/skeeey/kcp-integration/pkg/helpers"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type workspaceController struct {
	certFile        string
	keyFile         string
	kcpRestConfig   *rest.Config
	hubKubeClient   kubernetes.Interface
	workspaceLister cache.GenericLister
	hubs            map[string]context.CancelFunc
	eventRecorder   events.Recorder
}

//go:embed manifests
var manifestFiles embed.FS

var crds = []string{
	"manifests/hub/crds/managedclusteraddons.yaml",
	"manifests/hub/crds/managedclusters.yaml",
	"manifests/hub/crds/managedclustersetbindings.yaml",
	"manifests/hub/crds/managedclustersets.yaml",
	"manifests/hub/crds/manifestworks.yaml",
}

func NewWorkspaceController(
	certFile, keyFile string,
	kcpRestConfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder,
) factory.Controller {
	ctrl := &workspaceController{
		certFile:        certFile,
		keyFile:         keyFile,
		kcpRestConfig:   kcpRestConfig,
		hubKubeClient:   hubKubeClient,
		workspaceLister: workspaceInformer.Lister(),
		hubs:            map[string]context.CancelFunc{},
		eventRecorder:   recorder,
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

func (c *workspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()

	workspace, err := c.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return nil
	}

	klog.Infof("Reconcil workspace %s", workspaceName)

	workspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	workspaceConfig.Host = helpers.GetWorkspaceURL(workspace)

	// prepare hub crds
	if err := c.prepareHubCRDs(ctx, workspaceConfig); err != nil {
		return err
	}

	// start registration hub controllers for this workspace
	if err := c.startHubControllers(ctx, workspaceName, workspaceConfig); err != nil {
		return err
	}

	// TODO create rbac for this workspace

	// TODO start csr controller for this workspace

	return nil
}

func (c *workspaceController) prepareHubCRDs(ctx context.Context, restConfig *rest.Config) error {
	workspaceAPIExtensionClient, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	return helpers.ApplyObjects(
		ctx,
		nil,
		workspaceAPIExtensionClient,
		c.eventRecorder,
		manifestFiles,
		nil,
		crds...,
	)
}

func (c *workspaceController) startHubControllers(ctx context.Context, workspaceName string, config *rest.Config) error {
	if c.hubs[workspaceName] != nil {
		return nil
	}

	if err := hub.RunControllerManager(ctx, nil); err != nil {
		return err
	}
	return nil
}
