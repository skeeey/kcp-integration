package helpers

import (
	"context"
	"encoding/json"
	"fmt"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

type UpdateManagedClusterStatusFunc func(status *clusterv1.ManagedClusterStatus) error

func UpdateManagedClusterStatus(
	ctx context.Context,
	client clusterclient.Interface,
	spokeClusterName string,
	updateFuncs ...UpdateManagedClusterStatusFunc) (*clusterv1.ManagedClusterStatus, bool, error) {
	updated := false
	var updatedManagedClusterStatus *clusterv1.ManagedClusterStatus

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		managedCluster, err := client.ClusterV1().ManagedClusters().Get(ctx, spokeClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &managedCluster.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedManagedClusterStatus = newStatus
			return nil
		}

		oldData, err := json.Marshal(clusterv1.ManagedCluster{
			Status: *oldStatus,
		})

		if err != nil {
			return fmt.Errorf("failed to Marshal old data for cluster status %s: %w", managedCluster.Name, err)
		}

		newData, err := json.Marshal(clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				UID:             managedCluster.UID,
				ResourceVersion: managedCluster.ResourceVersion,
			}, // to ensure they appear in the patch as preconditions
			Status: *newStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal new data for cluster status %s: %w", managedCluster.Name, err)
		}

		patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
		if err != nil {
			return fmt.Errorf("failed to create patch for cluster %s: %w", managedCluster.Name, err)
		}

		updatedManagedCluster, err := client.ClusterV1().ManagedClusters().Patch(ctx, managedCluster.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")

		updatedManagedClusterStatus = &updatedManagedCluster.Status
		updated = err == nil
		return err
	})

	return updatedManagedClusterStatus, updated, err
}

func UpdateManagedClusterConditionFn(cond metav1.Condition) UpdateManagedClusterStatusFunc {
	return func(oldStatus *clusterv1.ManagedClusterStatus) error {
		meta.SetStatusCondition(&oldStatus.Conditions, cond)
		return nil
	}
}
