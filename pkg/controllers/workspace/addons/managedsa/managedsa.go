package managedsa

import (
	"github.com/skeeey/kcp-integration/pkg/helpers"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/managed-serviceaccount/pkg/addon/manager"
	"open-cluster-management.io/managed-serviceaccount/pkg/common"
	"open-cluster-management.io/managed-serviceaccount/pkg/features"

	runtime "sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	managedServiceAccountImage = "quay.io/open-cluster-management/managed-serviceaccount:latest"
)

func AddAddon(addOnCtx *helpers.AddOnManagerContext, addonManager addonmanager.AddonManager,
	ctrlManager runtime.Manager) error {
	agentAddOn, err := addonfactory.NewAgentAddonFactory(
		common.AddonName, manager.FS, "manifests/templates").
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			manager.GetDefaultValues(managedServiceAccountImage, nil),
			addonfactory.GetAddOnDeloymentConfigValues(
				addonfactory.NewAddOnDeloymentConfigGetter(addOnCtx.AddOnClient),
				addonfactory.ToAddOnDeloymentConfigValues,
			),
		).
		WithAgentRegistrationOption(manager.NewRegistrationOption(addOnCtx.KubeClient)).
		WithInstallStrategy(agent.InstallAllStrategy(common.AddonAgentInstallNamespace)).
		BuildTemplateAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(agentAddOn); err != nil {
		return err
	}

	if features.FeatureGates.Enabled(features.EphemeralIdentity) {
		if err := (manager.NewEphemeralIdentityReconciler(
			ctrlManager.GetCache(),
			ctrlManager.GetClient(),
		)).SetupWithManager(ctrlManager); err != nil {
			return err
		}
	}
	return nil
}
