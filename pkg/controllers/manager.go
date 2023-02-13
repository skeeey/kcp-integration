package controllers

import (
	"context"
	"time"

	"github.com/spf13/pflag"

	"github.com/skeeey/kcp-integration/pkg/controllers/cluster"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ManagerOptions defines the flags for kcp-ocm integration controller manager
type ManagerOptions struct {
	ControlPlaneKubeConfigFile string
	XCMServer                  string
}

// NewManagerOptions returns the flags with default value set
func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *ManagerOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(
		&o.ControlPlaneKubeConfigFile,
		"control-plane-kubeconfig",
		o.ControlPlaneKubeConfigFile,
		"Location of control plane kubeconfig file to connect to control plane cluster.",
	)

	flags.StringVar(
		&o.XCMServer,
		"xcm-server",
		o.XCMServer,
		"The host url of the xCM server.",
	)
}

// Run starts all of controllers for kcp-ocm integration
func (o *ManagerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	controlPlaneKubeConfig, err := clientcmd.BuildConfigFromFlags("", o.ControlPlaneKubeConfigFile)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(controlPlaneKubeConfig)
	if err != nil {
		return err
	}

	clusterClient, err := clusterclient.NewForConfig(controlPlaneKubeConfig)
	if err != nil {
		return err
	}

	kubeInfomer := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	clusterController := cluster.NewClusterController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		o.XCMServer,
		controllerContext.EventRecorder,
	)

	clusterAutoApproveController := cluster.NewClusterAutoApproveController(
		kubeClient,
		clusterClient,
		kubeInfomer.Certificates().V1().CertificateSigningRequests(),
		controllerContext.EventRecorder,
	)

	go kubeInfomer.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())

	go clusterController.Run(ctx, 1)
	go clusterAutoApproveController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
