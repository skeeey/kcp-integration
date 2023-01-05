package policy

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/skeeey/kcp-integration/pkg/helpers"

	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"

	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	automationctrl "open-cluster-management.io/governance-policy-propagator/controllers/automation"
	encryptionkeysctrl "open-cluster-management.io/governance-policy-propagator/controllers/encryptionkeys"
	metricsctrl "open-cluster-management.io/governance-policy-propagator/controllers/policymetrics"
	policysetctrl "open-cluster-management.io/governance-policy-propagator/controllers/policyset"
	propagatorctrl "open-cluster-management.io/governance-policy-propagator/controllers/propagator"
)

const (
	policyFrameworkAddonName        = "governance-policy-framework"
	configPolicyAddonName           = "config-policy-controller"
	evaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	prometheusEnabledAnnotation     = "prometheus-metrics-enabled"
	configPolicyControllerImage     = "quay.io/open-cluster-management/config-policy-controller:latest"
	kubeRBACProxyImage              = "registry.redhat.io/openshift4/ose-kube-rbac-proxy:v4.10"
	policyFrameworkAddonImage       = "quay.io/open-cluster-management/governance-policy-framework-addon:latest"
)

var agentPermissionFiles = []string{
	"manifests/hubpermissions/role.yaml",
	"manifests/hubpermissions/rolebinding.yaml",
}

func AddAddon(addOnCtx *helpers.AddOnManagerContext, addonManager addonmanager.AddonManager,
	ctrlManager manager.Manager) error {
	policyFrameworkAddon, err := addonfactory.NewAgentAddonFactory(
		policyFrameworkAddonName, policyframework.FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getPolicyFrameworkValues, addonfactory.GetValuesFromAddonAnnotation).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		WithAgentRegistrationOption(addon.NewRegistrationOption(
			addOnCtx.CtrlContext,
			policyFrameworkAddonName,
			agentPermissionFiles,
			policyframework.FS)).
		BuildHelmAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(policyFrameworkAddon); err != nil {
		return err
	}

	configPolicyAddon, err := addonfactory.NewAgentAddonFactory(
		configPolicyAddonName, configpolicy.FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getConfigPolicyValues, addonfactory.GetValuesFromAddonAnnotation).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		WithScheme(addon.Scheme).
		WithAgentRegistrationOption(addon.NewRegistrationOption(
			addOnCtx.CtrlContext,
			configPolicyAddonName,
			agentPermissionFiles,
			configpolicy.FS)).
		BuildHelmAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(configPolicyAddon); err != nil {
		return err
	}

	dynamicWatcherReconciler, dynamicWatcherSource := k8sdepwatches.NewControllerRuntimeSource()
	dynamicWatcher, err := k8sdepwatches.New(addOnCtx.CtrlContext.KubeConfig, dynamicWatcherReconciler, nil)
	if err != nil {
		return err
	}

	go func() {
		err := dynamicWatcher.Start(context.TODO())
		if err != nil {
			klog.Error(err, "Unable to start the dynamic watcher", "controller", propagatorctrl.ControllerName)
		}
	}()

	if err = (&propagatorctrl.PolicyReconciler{
		Client:         ctrlManager.GetClient(),
		Scheme:         ctrlManager.GetScheme(),
		Recorder:       ctrlManager.GetEventRecorderFor(propagatorctrl.ControllerName),
		DynamicWatcher: dynamicWatcher,
	}).SetupWithManager(ctrlManager, dynamicWatcherSource); err != nil {
		return err
	}

	if strings.EqualFold(os.Getenv("DISABLE_REPORT_METRICS"), "true") {
		if err = (&metricsctrl.MetricReconciler{
			Client: ctrlManager.GetClient(),
			Scheme: ctrlManager.GetScheme(),
		}).SetupWithManager(ctrlManager); err != nil {
			return err
		}
	}

	if err = (&automationctrl.PolicyAutomationReconciler{
		Client:        ctrlManager.GetClient(),
		DynamicClient: addOnCtx.DynamicClient,
		Scheme:        ctrlManager.GetScheme(),
		Recorder:      ctrlManager.GetEventRecorderFor(automationctrl.ControllerName),
	}).SetupWithManager(ctrlManager); err != nil {
		return err
	}

	if err = (&policysetctrl.PolicySetReconciler{
		Client:   ctrlManager.GetClient(),
		Scheme:   ctrlManager.GetScheme(),
		Recorder: ctrlManager.GetEventRecorderFor(policysetctrl.ControllerName),
	}).SetupWithManager(ctrlManager); err != nil {
		return err
	}

	// TODO: allow KeyRotationDays & MaxConcurrentReconciles configuration
	if err = (&encryptionkeysctrl.EncryptionKeysReconciler{
		Client:                  ctrlManager.GetClient(),
		KeyRotationDays:         30,
		MaxConcurrentReconciles: 10,
		Scheme:                  ctrlManager.GetScheme(),
	}).SetupWithManager(ctrlManager); err != nil {
		return err
	}

	propagatorctrl.Initialize(addOnCtx.CtrlContext.KubeConfig, &addOnCtx.KubeClient)

	cache := ctrlManager.GetCache()

	// The following index for the PlacementRef Name is being added to the
	// client cache to improve the performance of querying PlacementBindings
	indexFunc := func(obj client.Object) []string {
		return []string{obj.(*policyv1.PlacementBinding).PlacementRef.Name}
	}

	if err := cache.IndexField(
		context.TODO(), &policyv1.PlacementBinding{}, "placementRef.name", indexFunc,
	); err != nil {
		panic(err)
	}

	klog.Info("Waiting for the dynamic watcher to start")
	// This is important to avoid adding watches before the dynamic watcher is ready
	<-dynamicWatcher.Started()

	return nil
}

type userValues struct {
	OnMulticlusterHub bool               `json:"onMulticlusterHub"`
	GlobalValues      addon.GlobalValues `json:"global"`
	UserArgs          addon.UserArgs     `json:"args"`
}

func getPolicyFrameworkValues(cluster *clusterv1.ManagedCluster,
	policy *addonv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	userValues := userValues{
		OnMulticlusterHub: false,
		GlobalValues: addon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"governance_policy_framework_addon": policyFrameworkAddonImage,
			},
			NodeSelector: map[string]string{},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
		UserArgs: addon.UserArgs{
			LogEncoder:  "console",
			LogLevel:    0,
			PkgLogLevel: -1,
		},
	}
	// special case for local-cluster
	if cluster.Name == "local-cluster" {
		userValues.OnMulticlusterHub = true
	}

	if val, ok := policy.GetAnnotations()["addon.open-cluster-management.io/on-multicluster-hub"]; ok {
		if strings.EqualFold(val, "true") {
			userValues.OnMulticlusterHub = true
		} else if strings.EqualFold(val, "false") {
			// the special case can still be overridden by this annotation
			userValues.OnMulticlusterHub = false
		}
	}

	if val, ok := policy.GetAnnotations()[addon.PolicyLogLevelAnnotation]; ok {
		logLevel := addon.GetLogLevel(policyFrameworkAddonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	return addonfactory.JsonStructToValues(userValues)
}

func getConfigPolicyValues(cluster *clusterv1.ManagedCluster,
	policy *addonv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	configImage := os.Getenv("CONFIG_POLICY_CONTROLLER_IMAGE")
	if configImage == "" {
		configImage = configPolicyControllerImage
	}
	proxyImage := os.Getenv("KUBE_RBAC_PROXY_IMAGE")
	if proxyImage == "" {
		proxyImage = kubeRBACProxyImage
	}
	userValues := configpolicy.UserValues{
		GlobalValues: addon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"config_policy_controller": configImage,
				"kube_rbac_proxy":          proxyImage,
			},
			NodeSelector: map[string]string{},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
		Prometheus: map[string]interface{}{},
		UserArgs: configpolicy.UserArgs{
			UserArgs: addon.UserArgs{
				LogEncoder:  "console",
				LogLevel:    0,
				PkgLogLevel: -1,
			},
			EvaluationConcurrency: 2,
		},
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value
			break
		}
	}

	if val, ok := policy.GetAnnotations()[addon.PolicyLogLevelAnnotation]; ok {
		logLevel := addon.GetLogLevel(configPolicyAddonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	if val, ok := policy.GetAnnotations()[evaluationConcurrencyAnnotation]; ok {
		value, err := strconv.ParseUint(val, 10, 8)
		if err != nil {
			klog.Error(err, fmt.Sprintf(
				"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
				evaluationConcurrencyAnnotation, val, configPolicyAddonName, userValues.UserArgs.EvaluationConcurrency),
			)
		} else {
			// This is safe because we specified the uint8 in ParseUint
			userValues.UserArgs.EvaluationConcurrency = uint8(value)
		}
	}

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"
	if userValues.KubernetesDistribution == "OpenShift" {
		userValues.Prometheus["serviceMonitor"] = map[string]interface{}{"namespace": "openshift-monitoring"}
	}

	if val, ok := policy.GetAnnotations()[prometheusEnabledAnnotation]; ok {
		valBool, err := strconv.ParseBool(val)
		if err != nil {
			klog.Error(err, fmt.Sprintf(
				"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %v)",
				prometheusEnabledAnnotation, val, configPolicyAddonName, userValues.Prometheus["enabled"]),
			)
		} else {
			userValues.Prometheus["enabled"] = valBool
		}
	}

	return addonfactory.JsonStructToValues(userValues)
}
