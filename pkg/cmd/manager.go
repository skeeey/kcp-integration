package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/skeeey/kcp-integration/pkg/controllers"
	"github.com/skeeey/kcp-integration/pkg/version"
)

// NewManager generates a command to start kcp-ocm integration controller manager
func NewManager() *cobra.Command {
	o := controllers.NewManagerOptions()
	cmdConfig := controllercmd.NewControllerCommandConfig("xcm-connector", version.Get(), o.Run)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the xCM connector"

	flags := cmd.Flags()
	o.AddFlags(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the controller.")
	return cmd
}
