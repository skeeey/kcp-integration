package clustermanager

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestXxx(t *testing.T) {
	workspace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tenancy.kcp.dev/v1alpha1",
			"kind":       "ClusterWorkspace",
			"metadata": map[string]interface{}{
				"name": "hub",
			},
			"spec": map[string]interface{}{
				"type": "Ocmhub",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"lastTransitionTime": "2022-06-13T07:48:37Z",
						"status":             "True",
						"type":               "WorkspaceShardValid",
					},
					map[string]interface{}{
						"lastTransitionTime": "2022-06-13T07:48:37Z",
						"status":             "True",
						"type":               "WorkspaceScheduled",
					},
				},
				"phase": "Ready",
			},
		},
	}

	conditionSlice, _, err := unstructured.NestedSlice(workspace.Object, "status", "conditions")
	if err != nil {
		t.Errorf("%v", err)
	}

	conditions := toConditions(conditionSlice)

	appliedConditon := &metav1.Condition{
		Type:    clusterManagerApplied,
		Reason:  "ClusterManagerApplied",
		Status:  metav1.ConditionTrue,
		Message: "ClusterManager is applied",
	}

	meta.SetStatusCondition(&conditions, *appliedConditon)

	conditionSlice = toConditionSlice(conditions)

	if err := unstructured.SetNestedSlice(workspace.Object, conditionSlice, "status", "conditions"); err != nil {
		t.Errorf("%v", err)
	}

	t.Errorf("%+v", workspace.Object)

}
