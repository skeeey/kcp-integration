package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	msafeatures "open-cluster-management.io/managed-serviceaccount/pkg/features"

	"github.com/skeeey/kcp-integration/pkg/controllers"
	"github.com/skeeey/kcp-integration/pkg/version"
)

// NewManager generates a command to start kcp-ocm integration controller manager
func NewManager() *cobra.Command {
	o := controllers.NewManagerOptions()
	cmdConfig := controllercmd.NewControllerCommandConfig("kcp-integration-controller", version.Get(), o.Run)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the kcp integration controller"

	flags := cmd.Flags()
	msafeatures.FeatureGates.AddFlag(flags)
	o.AddFlags(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the controller.")
	return cmd
}
