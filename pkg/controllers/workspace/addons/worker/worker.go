package worker

import (
	"github.com/skeeey/kcp-integration/pkg/helpers"

	"github.com/stolostron/multicloud-operators-foundation/pkg/addon"
	"github.com/stolostron/multicloud-operators-foundation/pkg/controllers/clusterinfo"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	workerImage   = "quay.io/stolostron/multicloud-manager:latest"
	logCertSecret = "open-cluster-management/ocm-klusterlet-self-signed-secrets"
)

func AddAddon(ctx *helpers.WorkspaceContext, addonManager addonmanager.AddonManager,
	ctrlManager manager.Manager) error {
	agentAddon, err := addonfactory.NewAgentAddonFactory(addon.WorkManagerAddonName, addon.ChartFS, addon.ChartDir).
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			addon.NewGetValuesFunc(workerImage),
			addonfactory.GetValuesFromAddonAnnotation,
			addonfactory.GetAddOnDeloymentConfigValues(
				addonfactory.NewAddOnDeloymentConfigGetter(ctx.AddOnClient),
				addonfactory.ToAddOnNodePlacementValues,
			),
		).
		WithAgentRegistrationOption(addon.NewRegistrationOption(ctx.KubeClient, addon.WorkManagerAddonName)).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		BuildHelmAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(agentAddon); err != nil {
		return err
	}

	return clusterinfo.SetupWithManager(ctrlManager, logCertSecret)
}
