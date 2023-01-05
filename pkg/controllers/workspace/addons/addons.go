package addons

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	policyv1beta1 "open-cluster-management.io/governance-policy-propagator/api/v1beta1"
	msav1alpha1 "open-cluster-management.io/managed-serviceaccount/api/v1alpha1"
	msamanager "open-cluster-management.io/managed-serviceaccount/pkg/addon/manager"
	msacommon "open-cluster-management.io/managed-serviceaccount/pkg/common"
	msafeatures "open-cluster-management.io/managed-serviceaccount/pkg/features"
	placementrulev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"

	clusterinfov1beta1 "github.com/stolostron/cluster-lifecycle-api/clusterinfo/v1beta1"
	worker "github.com/stolostron/multicloud-operators-foundation/pkg/addon"
	"github.com/stolostron/multicloud-operators-foundation/pkg/controllers/clusterinfo"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type AddOnManagerContext struct {
	RestConfig  *rest.Config
	KubeClient  kubernetes.Interface
	AddOnClient addonclientset.Interface
}

const (
	workerImage                = "quay.io/stolostron/multicloud-manager:latest"
	managedServiceAccountImage = "quay.io/open-cluster-management/managed-serviceaccount:latest"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(kubescheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterinfov1beta1.AddToScheme(scheme))
	utilruntime.Must(msav1alpha1.AddToScheme(scheme))
	utilruntime.Must(policyv1.AddToScheme(scheme))
	utilruntime.Must(policyv1beta1.AddToScheme(scheme))
	utilruntime.Must(placementrulev1.AddToScheme(scheme))
}

func StartAddOnManagers(ctx context.Context, addOnCtx *AddOnManagerContext) {
	ctrl.SetLogger(klogr.New())

	addonManager, err := addonmanager.New(addOnCtx.RestConfig)
	if err != nil {
		klog.Errorf("unable to create addon manager %v", err)
		return
	}

	ctrlManager, err := ctrl.NewManager(addOnCtx.RestConfig, ctrl.Options{Scheme: scheme})
	if err != nil {
		klog.Errorf("unable to create controller-runtime manager %v", err)
		return
	}

	// add work addon manager
	if err := addWorkerAddOn(addonManager, ctrlManager, addOnCtx); err != nil {
		klog.Errorf("unable to add work addon manager %v", err)
		return
	}

	if err := addManagedServiceAccountAddon(addonManager, ctrlManager, addOnCtx); err != nil {
		klog.Errorf("unable to add managed-serviceaccount addon manager %v", err)
		return
	}

	if err := addonManager.Start(ctx); err != nil {
		klog.Errorf("failed to start addon manager: %v", err)
		return
	}

	if err := ctrlManager.Start(ctx); err != nil {
		klog.Errorf("unable to start controller-runtime manager %v", err)
		return
	}
}

func addWorkerAddOn(addonManager addonmanager.AddonManager, ctrlManager manager.Manager,
	addOnCtx *AddOnManagerContext) error {
	agentAddon, err := addonfactory.NewAgentAddonFactory(worker.WorkManagerAddonName, worker.ChartFS, worker.ChartDir).
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			worker.NewGetValuesFunc(workerImage),
			addonfactory.GetValuesFromAddonAnnotation,
			addonfactory.GetAddOnDeloymentConfigValues(
				addonfactory.NewAddOnDeloymentConfigGetter(addOnCtx.AddOnClient),
				addonfactory.ToAddOnNodePlacementValues,
			),
		).
		WithAgentRegistrationOption(worker.NewRegistrationOption(addOnCtx.KubeClient, worker.WorkManagerAddonName)).
		WithInstallStrategy(agent.InstallAllStrategy("open-cluster-management-agent-addon")).
		BuildHelmAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(agentAddon); err != nil {
		return err
	}

	return clusterinfo.SetupWithManager(ctrlManager, "open-cluster-management/ocm-klusterlet-self-signed-secrets")
}

func addManagedServiceAccountAddon(addonManager addonmanager.AddonManager, ctrlManager manager.Manager,
	addOnCtx *AddOnManagerContext) error {
	agentAddOn, err := addonfactory.NewAgentAddonFactory(
		msacommon.AddonName, msamanager.FS, "manifests/templates").
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			msamanager.GetDefaultValues(managedServiceAccountImage, nil),
			addonfactory.GetAddOnDeloymentConfigValues(
				addonfactory.NewAddOnDeloymentConfigGetter(addOnCtx.AddOnClient),
				addonfactory.ToAddOnDeloymentConfigValues,
			),
		).
		WithAgentRegistrationOption(msamanager.NewRegistrationOption(addOnCtx.KubeClient)).
		WithInstallStrategy(agent.InstallAllStrategy(msacommon.AddonAgentInstallNamespace)).
		BuildTemplateAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(agentAddOn); err != nil {
		return err
	}

	if msafeatures.FeatureGates.Enabled(msafeatures.EphemeralIdentity) {
		if err := (msamanager.NewEphemeralIdentityReconciler(
			ctrlManager.GetCache(),
			ctrlManager.GetClient(),
		)).SetupWithManager(ctrlManager); err != nil {
			return err
		}
	}
	return nil
}
