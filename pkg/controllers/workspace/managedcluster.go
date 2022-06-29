package workspace

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/skeeey/kcp-integration/pkg/helpers"

	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var (
	orgWorkspaceAuthorizerFiles = []string{
		"manifests/workspace/org-workspace-clusterrole.yaml",
		"manifests/workspace/org-workspace-clusterrolebinding.yaml",
	}

	hubWorkspaceAuthorizerFiles = []string{
		"manifests/workspace/hub-workspace-clusterrole.yaml",
		"manifests/workspace/hub-workspace-clusterrolebinding.yaml",
	}
)

type managedClusterController struct {
	orgWorkspaceName     string
	workspaceName        string
	kcpRestConfig        *rest.Config
	managedClusterLister clusterlister.ManagedClusterLister
	eventRecorder        events.Recorder
}

func NewClusterController(
	orgWorkspaceName, workspaceName string,
	kcpRestConfig *rest.Config,
	clusterInformers clusterinformers.ManagedClusterInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &managedClusterController{
		orgWorkspaceName:     orgWorkspaceName,
		workspaceName:        workspaceName,
		kcpRestConfig:        kcpRestConfig,
		managedClusterLister: clusterInformers.Lister(),
		eventRecorder:        recorder.WithComponentSuffix("syncer-cluster-controller"),
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			clusterInformers.Informer(),
		).
		WithSync(c.sync).ToController("syncer-cluster-controller", recorder)
}

func (c *managedClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()

	clusterName := key
	klog.V(4).Infof("reconcil cluster %s", clusterName)

	_, err := c.managedClusterLister.Get(clusterName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// prepare authorizers for workspaces
	// refer to https://github.com/kcp-dev/kcp/blob/main/docs/authorization.md
	if err := c.prepareOrgWorkspaceAuthorizers(ctx, clusterName); err != nil {
		return err
	}

	return c.prepareHubWorkspaceAuthorizers(ctx, clusterName)
}

func (c *managedClusterController) prepareOrgWorkspaceAuthorizers(ctx context.Context, clusterName string) error {
	kcpRootKubeClient, err := kubernetes.NewForConfig(c.kcpRestConfig)
	if err != nil {
		return err
	}

	config := struct {
		OrgWorkspaceName string
		ClusterName      string
	}{
		OrgWorkspaceName: c.orgWorkspaceName,
		ClusterName:      clusterName,
	}

	return helpers.ApplyObjects(
		ctx,
		kcpRootKubeClient,
		nil,
		c.eventRecorder,
		manifestFiles,
		config,
		orgWorkspaceAuthorizerFiles...,
	)
}

func (c *managedClusterController) prepareHubWorkspaceAuthorizers(ctx context.Context, clusterName string) error {
	hubWorkspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	hubWorkspaceConfig.Host = fmt.Sprintf("%s:%s", c.kcpRestConfig.Host, c.orgWorkspaceName)

	kcpHubKubeClient, err := kubernetes.NewForConfig(hubWorkspaceConfig)
	if err != nil {
		return err
	}

	config := struct {
		OrgWorkspaceName string
		WorkspaceName    string
		ClusterName      string
	}{
		OrgWorkspaceName: c.orgWorkspaceName,
		WorkspaceName:    c.workspaceName,
		ClusterName:      clusterName,
	}

	return helpers.ApplyObjects(
		ctx,
		kcpHubKubeClient,
		nil,
		c.eventRecorder,
		manifestFiles,
		config,
		hubWorkspaceAuthorizerFiles...,
	)
}
