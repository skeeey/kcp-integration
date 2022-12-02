package helpers

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(corev1.AddToScheme(genericScheme))
	utilruntime.Must(rbacv1.AddToScheme(genericScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
}

var ClusterWorkspaceGVR = schema.GroupVersionResource{
	Group:    "tenancy.kcp.dev",
	Version:  "v1alpha1",
	Resource: "clusterworkspaces",
}

func GetWorkspaceType(workspace runtime.Object) string {
	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	workspaceTypeReference, found, err := unstructured.NestedMap(unstructuredWorkspace, "spec", "type")
	if err != nil {
		panic(err)
	}

	if !found {
		return ""
	}

	workspaceType, found, err := unstructured.NestedString(workspaceTypeReference, "name")
	if err != nil {
		panic(err)
	}

	if !found {
		return ""
	}

	return workspaceType
}

func GetWorkspacePhase(workspace runtime.Object) string {
	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	phase, found, err := unstructured.NestedString(unstructuredWorkspace, "status", "phase")
	if err != nil {
		panic(err)
	}

	if !found {
		return ""
	}

	return phase
}

func GetWorkspaceURL(workspace runtime.Object) string {
	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	url, found, err := unstructured.NestedString(unstructuredWorkspace, "status", "baseURL")
	if err != nil {
		panic(err)
	}

	if !found {
		return ""
	}

	return url
}

func IsWorkspaceStatusConditionTrue(workspace runtime.Object, conditionType string) bool {
	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	conditions, found, err := unstructured.NestedSlice(unstructuredWorkspace, "status", "conditions")
	if err != nil {
		panic(err)
	}

	if !found {
		return false
	}

	return meta.IsStatusConditionTrue(ToConditions(conditions), conditionType)
}

func ToConditions(slice []interface{}) []metav1.Condition {
	conditions := []metav1.Condition{}
	for _, item := range slice {
		data, err := json.Marshal(&item)
		if err != nil {
			panic(err)
		}

		strMap := map[string]interface{}{}
		if err := json.Unmarshal(data, &strMap); err != nil {
			panic(err)
		}

		condition := metav1.Condition{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(strMap, &condition); err != nil {
			panic(err)
		}

		conditions = append(conditions, condition)
	}

	return conditions
}

func ToConditionSlice(conditions []metav1.Condition) []interface{} {
	slice := []interface{}{}
	for _, condition := range conditions {
		conditionMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&condition)
		if err != nil {
			panic(err)
		}
		slice = append(slice, conditionMap)
	}
	return slice
}

func Indent(indention int, v []byte) string {
	newline := "\n" + strings.Repeat(" ", indention)
	return strings.Replace(string(v), "\n", newline, -1)
}

func ApplyObjects(ctx context.Context,
	kubeClient kubernetes.Interface,
	apiExtensionsClient apiextensionsclient.Interface,
	recorder events.Recorder,
	manifests embed.FS,
	config interface{},
	fileNames ...string) error {
	errs := []error{}

	objs := []runtime.Object{}
	for _, fileName := range fileNames {
		template, err := manifests.ReadFile(fileName)
		if err != nil {
			panic(err)
		}

		objs = append(objs, mustCreateObjectFromTemplate(fileName, template, config))
	}

	for _, obj := range objs {
		switch required := obj.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			_, _, err := resourceapply.ApplyCustomResourceDefinitionV1(ctx, apiExtensionsClient.ApiextensionsV1(), recorder, required)
			errs = append(errs, err)
		case *corev1.Namespace:
			_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), recorder, required)
			errs = append(errs, err)
		case *corev1.ConfigMap:
			_, _, err := resourceapply.ApplyConfigMap(ctx, kubeClient.CoreV1(), recorder, required)
			errs = append(errs, err)
		case *corev1.Secret:
			_, _, err := resourceapply.ApplySecret(ctx, kubeClient.CoreV1(), recorder, required)
			errs = append(errs, err)
		case *corev1.Service:
			_, _, err := resourceapply.ApplyService(ctx, kubeClient.CoreV1(), recorder, required)
			errs = append(errs, err)
		case *rbacv1.ClusterRole:
			_, _, err := resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), recorder, required)
			errs = append(errs, err)
		case *rbacv1.ClusterRoleBinding:
			_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), recorder, required)
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func mustCreateObjectFromTemplate(name string, tb []byte, config interface{}) runtime.Object {
	tmpl, err := template.New(name).Parse(string(tb))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		panic(err)
	}

	obj, _, err := genericCodec.Decode(buf.Bytes(), nil, nil)
	if err != nil {
		panic(err)
	}

	return obj
}
