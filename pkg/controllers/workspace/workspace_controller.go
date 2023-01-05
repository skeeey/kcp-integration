package workspace

import (
	"context"
	"embed"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	placement "open-cluster-management.io/placement/pkg/controllers"
	registration "open-cluster-management.io/registration/pkg/hub"

	"github.com/skeeey/kcp-integration/pkg/controllers/workspace/addons"
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
	csrControllers  map[string]context.CancelFunc
	eventRecorder   events.Recorder
}

//go:embed manifests
var manifestFiles embed.FS

var crds = []string{
	"manifests/hub/crds/addonplacementscores.yaml",
	"manifests/hub/crds/managedclusteraddons.yaml",
	"manifests/hub/crds/managedclusters.yaml",
	"manifests/hub/crds/managedclustersetbindings.yaml",
	"manifests/hub/crds/managedclustersets.yaml",
	"manifests/hub/crds/manifestworks.yaml",
	"manifests/hub/crds/placementdecisions.yaml",
	"manifests/hub/crds/placements.yaml",
}

var workspaceRBACs = []string{
	"manifests/workspace/workspace-clusterrolebinding.yaml",
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
		csrControllers:  map[string]context.CancelFunc{},
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

	// TODO put webhook in the kcp server
	// start registration hub controllers for this workspace
	if err := c.startHubControllers(ctx, workspaceName, workspaceConfig); err != nil {
		return err
	}

	// TODO put this in the kcp server
	// start csr controller for this workspace
	return c.startCSRControllers(ctx, workspaceName, c.kcpRestConfig, workspaceConfig)
}

func (c *workspaceController) prepareHubCRDs(ctx context.Context, config *rest.Config) error {
	workspaceAPIExtensionClient, err := apiextensionsclient.NewForConfig(config)
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

func (c *workspaceController) startHubControllers(ctx context.Context, name string, config *rest.Config) error {
	if c.hubs[name] != nil {
		return nil
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	addOnClient, err := addonclientset.NewForConfig(config)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)
	c.hubs[name] = cancel

	ctrlCtx := &controllercmd.ControllerContext{
		KubeConfig:        config,
		EventRecorder:     c.eventRecorder.ForComponent(name),
		OperatorNamespace: "open-cluster-management-hub",
	}

	addOnCtx := &helpers.AddOnManagerContext{
		CtrlContext: ctrlCtx,
		KubeClient:  kubeClient,
		AddOnClient: addOnClient,
	}

	go func(ctx context.Context, controllerContext *controllercmd.ControllerContext) {
		if err := registration.RunControllerManager(ctx, controllerContext); err != nil {
			klog.Errorf("failed to start hub for workspace %q, %v", name, err)
			if cancel, ok := c.hubs[name]; ok {
				cancel()
			}
			delete(c.hubs, name)
		}
	}(workspaceCtx, ctrlCtx)

	go func(ctx context.Context, controllerContext *controllercmd.ControllerContext) {
		if err := placement.RunControllerManager(ctx, controllerContext); err != nil {
			klog.Errorf("failed to start hub for workspace %q, %v", name, err)
			if cancel, ok := c.hubs[name]; ok {
				cancel()
			}
			delete(c.hubs, name)
		}
	}(workspaceCtx, ctrlCtx)

	// TODO should handle error
	go addons.StartAddOnManagers(workspaceCtx, addOnCtx)

	return nil
}

func (c *workspaceController) startCSRControllers(
	ctx context.Context, name string, rootConfig, workspaceConfig *rest.Config) error {
	if c.csrControllers[name] != nil {
		return nil
	}

	kubeClient, err := kubernetes.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)
	c.csrControllers[name] = cancel

	kubeInfomer := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)

	csrSigningController := NewCSRSigningController(
		name,
		c.certFile,
		c.keyFile,
		kubeClient,
		rootConfig,
		kubeInfomer.Certificates().V1().CertificateSigningRequests(),
		c.eventRecorder,
	)

	go kubeInfomer.Start(workspaceCtx.Done())

	go csrSigningController.Run(workspaceCtx, 1)

	return nil
}
