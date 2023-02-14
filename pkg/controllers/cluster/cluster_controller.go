package cluster

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/skeeey/kcp-integration/pkg/helpers"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

const ManagedClusterConditionConnected string = "ManagedClusterConditionConnected"

type clusterController struct {
	clusterClient clusterclient.Interface
	clusterLister clusterlisterv1.ManagedClusterLister
	xCMAPIServer  string
}

func NewClusterController(
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	xCMAPIServer string,
	recorder events.Recorder,
) factory.Controller {
	ctrl := &clusterController{
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		xCMAPIServer:  xCMAPIServer,
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			clusterInformer.Informer(),
		).
		WithSync(ctrl.sync).
		ToController("cluster-controller", recorder)
}

func (c *clusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusterName := syncCtx.QueueKey()
	klog.Infof("Reconciling ManagedCluster %s", managedClusterName)

	cluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// TODO timeout or retry limits
	if err := helpers.CreateCluster(c.xCMAPIServer, cluster); err != nil {
		return err
	}

	if cond := meta.FindStatusCondition(cluster.Status.Conditions, ManagedClusterConditionConnected); cond != nil {
		return nil
	}

	conditionUpdateFn := helpers.UpdateManagedClusterConditionFn(metav1.Condition{
		Type:    ManagedClusterConditionConnected,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterConnected",
		Message: fmt.Sprintf("Managed cluster %s is connected to xCM.", cluster.Name),
	})

	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.clusterClient, cluster.Name, conditionUpdateFn)
	if updated {
		syncCtx.Recorder().Eventf("ManagedClusterConnectedConditionUpdated",
			"update managed cluster %q connected condition to true",
			cluster.Name)
	}

	return err
}
