package clustermanager

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/skeeey/kcp-integration/pkg/helpers"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
)

const (
	clusterManagerApplied = "ClusterManagerApplied"
	workspaceAnnotation   = "kcp-integration.open-cluster-management.io/workspace"
	caBundleConfigmapName = "ca-bundle-configmap"
	caBundleConfigmapKey  = "ca-bundle.crt"
)

//go:embed manifests
var manifestFiles embed.FS

var webhookFiles = []string{
	"manifests/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml",
	"manifests/cluster-manager-registration-webhook-mutatingconfiguration.yaml",
	"manifests/cluster-manager-registration-webhook-validatingconfiguration.yaml",
	"manifests/cluster-manager-work-webhook-validatingconfiguration.yaml",
}

type clusterManagerController struct {
	kcpRestConfig           *rest.Config
	hubKubeClient           kubernetes.Interface
	clusterManagerClient    operatorv1client.ClusterManagerInterface
	clusterManagerLister    operatorlister.ClusterManagerLister
	registrationWebhookHost string
	workWebhookHost         string
}

func NewClusterManagerController(
	kcpRestConfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	registrationWebhookHost, workWebhookHost string,
	recorder events.Recorder,
) factory.Controller {
	controller := &clusterManagerController{
		kcpRestConfig:           kcpRestConfig,
		hubKubeClient:           hubKubeClient,
		clusterManagerClient:    clusterManagerClient,
		clusterManagerLister:    clusterManagerInformer.Lister(),
		registrationWebhookHost: registrationWebhookHost,
		workWebhookHost:         workWebhookHost,
	}

	return factory.New().WithSync(controller.sync).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			name := accessor.GetName()
			return strings.HasPrefix(name, "kcp-")
		}, clusterManagerInformer.Informer()).
		ToController("ClusterManagerSyncController", recorder)
}

func (c *clusterManagerController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	clusterManager, err := c.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	// TODO handle cluster manager deletion case

	workspaceId, ok := clusterManager.Annotations[workspaceAnnotation]
	if !ok {
		// no workspace annotation, ignore
		return nil
	}

	workspaceIds := strings.Split(workspaceId, ":")
	if len(workspaceIds) != 2 {
		return fmt.Errorf("unexpected format for workspace annotation, <org-worksapce>:<ocm-hub-workspace> is required")
	}

	appliedConditon := isClusterManagerApplied(clusterManager)
	if appliedConditon == nil {
		// no operator to handle this clustermanager, ignore
		return nil
	}

	if appliedConditon.Status == metav1.ConditionFalse {
		return c.updateWorksapceStatus(ctx, controllerContext, workspaceIds[0], workspaceIds[1], *appliedConditon)
	}

	// clusterManger is deployed, apply the webhooks in the kcp workspace
	if err := c.applyWebhooks(ctx, controllerContext, clusterManager.Name, workspaceId); err != nil {
		return err
	}

	return c.updateWorksapceStatus(ctx, controllerContext, workspaceIds[0], workspaceIds[1], *appliedConditon)
}

func (c *clusterManagerController) updateWorksapceStatus(ctx context.Context,
	ctrlCtx factory.SyncContext,
	orgWorkspaceName, worksapceName string,
	appliedConditon metav1.Condition) error {
	workspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	workspaceConfig.Host = fmt.Sprintf("%s:%s", workspaceConfig.Host, orgWorkspaceName)

	dynamicClient, err := dynamic.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	workspace, err := dynamicClient.Resource(helpers.ClusterWorkspaceGVR).Get(ctx, worksapceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	workspace = workspace.DeepCopy()
	conditionSlice, found, err := unstructured.NestedSlice(workspace.Object, "status", "conditions")
	if err != nil {
		return err
	}

	if !found {
		conditionSlice = []interface{}{}
	}

	oldConditions := helpers.ToConditions(conditionSlice)

	newConditions := append([]metav1.Condition{}, oldConditions...)
	meta.SetStatusCondition(&newConditions, appliedConditon)

	if equality.Semantic.DeepEqual(oldConditions, newConditions) {
		return nil
	}

	newConditionSlice := helpers.ToConditionSlice(newConditions)
	if err := unstructured.SetNestedSlice(workspace.Object, newConditionSlice, "status", "conditions"); err != nil {
		return err
	}

	if _, err := dynamicClient.Resource(helpers.ClusterWorkspaceGVR).
		UpdateStatus(ctx, workspace, metav1.UpdateOptions{}); err != nil {
		return err
	}

	ctrlCtx.Recorder().Eventf("UpdateWorkspaceStatus", "The workspace root:%s:%s %s status was updated, due to %s",
		orgWorkspaceName, worksapceName, clusterManagerApplied, appliedConditon.Reason)
	return nil
}

func (c *clusterManagerController) applyWebhooks(ctx context.Context,
	ctrlCtx factory.SyncContext,
	clusterManagerName, workspaceId string) error {
	caBundleConfigMap, err := c.hubKubeClient.CoreV1().
		ConfigMaps(clusterManagerName).Get(ctx, caBundleConfigmapName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	caBundle, ok := caBundleConfigMap.Data[caBundleConfigmapKey]
	if !ok {
		return fmt.Errorf("the ca bundle is not found in %s/%s", clusterManagerName, caBundleConfigmapKey)
	}

	regSvc, err := c.hubKubeClient.CoreV1().
		Services(clusterManagerName).Get(ctx, "cluster-manager-registration-webhook", metav1.GetOptions{})
	if err != nil {
		return err
	}

	workSvc, err := c.hubKubeClient.CoreV1().
		Services(clusterManagerName).Get(ctx, "cluster-manager-work-webhook", metav1.GetOptions{})
	if err != nil {
		return err
	}

	workspaceConfig := rest.CopyConfig(c.kcpRestConfig)
	workspaceConfig.Host = fmt.Sprintf("%s:%s", workspaceConfig.Host, workspaceId)

	workspaceKubeClient, err := kubernetes.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	config := struct {
		RegistrationWebhookHost string
		WorkWebhookHost         string
		CABundle                string
	}{
		RegistrationWebhookHost: fmt.Sprintf("https://%s:%d", c.registrationWebhookHost, regSvc.Spec.Ports[0].NodePort),
		WorkWebhookHost:         fmt.Sprintf("https://%s:%d", c.workWebhookHost, workSvc.Spec.Ports[0].NodePort),
		CABundle:                base64.StdEncoding.EncodeToString([]byte(caBundle)),
	}

	return helpers.ApplyObjects(
		ctx,
		workspaceKubeClient,
		nil,
		ctrlCtx.Recorder(),
		manifestFiles,
		config,
		webhookFiles...,
	)
}

func isClusterManagerApplied(clusterManager *operatorv1.ClusterManager) *metav1.Condition {
	appliedConditon := &metav1.Condition{
		Type:    clusterManagerApplied,
		Reason:  "ClusterManagerApplied",
		Status:  metav1.ConditionTrue,
		Message: "ClusterManager is applied",
	}

	if !meta.IsStatusConditionTrue(clusterManager.Status.Conditions, "Applied") {
		condition := meta.FindStatusCondition(clusterManager.Status.Conditions, "Applied")
		if condition == nil {
			return nil
		}

		appliedConditon.Reason = "ClusterManagerApplyFailed"
		appliedConditon.Message = condition.Message
		return appliedConditon
	}

	if meta.IsStatusConditionTrue(clusterManager.Status.Conditions, "HubRegistrationDegraded") {
		condition := meta.FindStatusCondition(clusterManager.Status.Conditions, "HubRegistrationDegraded")
		if condition == nil {
			return nil
		}

		appliedConditon.Reason = "ClusterManagerApplyDegraded"
		appliedConditon.Message = condition.Message
		return appliedConditon
	}

	if meta.IsStatusConditionTrue(clusterManager.Status.Conditions, "HubPlacementDegraded") {
		condition := meta.FindStatusCondition(clusterManager.Status.Conditions, "HubPlacementDegraded")
		if condition == nil {
			return nil
		}

		appliedConditon.Reason = "ClusterManagerApplyDegraded"
		appliedConditon.Message = condition.Message
		return appliedConditon
	}

	return appliedConditon
}
