package policy

import (
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
	"open-cluster-management.io/governance-policy-propagator/controllers/automation"
	"open-cluster-management.io/governance-policy-propagator/controllers/encryptionkeys"
	"open-cluster-management.io/governance-policy-propagator/controllers/policymetrics"
	policysetctrl "open-cluster-management.io/governance-policy-propagator/controllers/policyset"
	"open-cluster-management.io/governance-policy-propagator/controllers/propagator"
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

func AddAddon(ctx *helpers.WorkspaceContext, addonManager addonmanager.AddonManager,
	ctrlManager manager.Manager) error {
	policyFrameworkAddon, err := addonfactory.NewAgentAddonFactory(
		policyFrameworkAddonName, policyframework.FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getPolicyFrameworkValues, addonfactory.GetValuesFromAddonAnnotation).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		WithAgentRegistrationOption(addon.NewRegistrationOption(
			ctx.CtrlContext,
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
			ctx.CtrlContext,
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
	dynamicWatcher, err := k8sdepwatches.New(ctx.CtrlContext.KubeConfig, dynamicWatcherReconciler, nil)
	if err != nil {
		return err
	}

	go func() {
		if err := dynamicWatcher.Start(ctx.Context); err != nil {
			klog.Error(err, "Unable to start the dynamic watcher", "controller", propagator.ControllerName)
		}
	}()

	propagatorCtrl := &propagator.PolicyReconciler{
		Client:         ctrlManager.GetClient(),
		Scheme:         ctrlManager.GetScheme(),
		Recorder:       ctrlManager.GetEventRecorderFor(propagator.ControllerName),
		DynamicWatcher: dynamicWatcher,
	}
	if err := propagatorCtrl.SetupWithManager(ctrlManager, dynamicWatcherSource); err != nil {
		return err
	}

	if strings.EqualFold(os.Getenv("ENABLE_REPORT_METRICS"), "true") {
		policymetricsCtrl := &policymetrics.MetricReconciler{
			Client: ctrlManager.GetClient(),
			Scheme: ctrlManager.GetScheme(),
		}
		if err := policymetricsCtrl.SetupWithManager(ctrlManager); err != nil {
			return err
		}
	}

	automationCtrl := &automation.PolicyAutomationReconciler{
		Client:        ctrlManager.GetClient(),
		DynamicClient: ctx.DynamicClient,
		Scheme:        ctrlManager.GetScheme(),
		Recorder:      ctrlManager.GetEventRecorderFor(automation.ControllerName),
	}
	if err := automationCtrl.SetupWithManager(ctrlManager); err != nil {
		return err
	}

	policysetCtrl := &policysetctrl.PolicySetReconciler{
		Client:   ctrlManager.GetClient(),
		Scheme:   ctrlManager.GetScheme(),
		Recorder: ctrlManager.GetEventRecorderFor(policysetctrl.ControllerName),
	}
	if err := policysetCtrl.SetupWithManager(ctrlManager); err != nil {
		return err
	}

	encryptionkeysCtrl := &encryptionkeys.EncryptionKeysReconciler{
		Client:                  ctrlManager.GetClient(),
		KeyRotationDays:         30,
		MaxConcurrentReconciles: 10,
		Scheme:                  ctrlManager.GetScheme(),
	}
	if err := encryptionkeysCtrl.SetupWithManager(ctrlManager); err != nil {
		return err
	}

	propagator.Initialize(ctx.CtrlContext.KubeConfig, &ctx.KubeClient)

	// The following index for the PlacementRef Name is being added to the
	// client cache to improve the performance of querying PlacementBindings
	if err := ctrlManager.GetCache().IndexField(
		ctx.Context, &policyv1.PlacementBinding{}, "placementRef.name", func(obj client.Object) []string {
			return []string{obj.(*policyv1.PlacementBinding).PlacementRef.Name}
		},
	); err != nil {
		return err
	}

	klog.Info("Waiting for the dynamic watcher to start")
	// This is important to avoid adding watches before the dynamic watcher is ready
	<-dynamicWatcher.Started()
	klog.Info("the dynamic watcher is started")

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
	userValues := configpolicy.UserValues{
		GlobalValues: addon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"config_policy_controller": configPolicyControllerImage,
				"kube_rbac_proxy":          kubeRBACProxyImage,
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
