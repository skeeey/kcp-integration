package controllers

import (
	"context"
	"time"

	"github.com/skeeey/kcp-integration/pkg/controllers/workspace"
	"github.com/skeeey/kcp-integration/pkg/helpers"
	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

// ManagerOptions defines the flags for kcp-ocm integration controller manager
type ManagerOptions struct {
	KCPKubeConfigFile  string
	KCPSigningCertFile string
	KCPSigningKeyFile  string
}

// NewManagerOptions returns the flags with default value set
func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *ManagerOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.KCPKubeConfigFile, "kcp-kubeconfig", o.KCPKubeConfigFile, "Location of kcp kubeconfig file to connect to kcp root cluster.")
	flags.StringVar(&o.KCPSigningCertFile, "kcp-signing-cert-file", o.KCPSigningCertFile, "Location of CA certificate file used to issue certificates in kcp.")
	flags.StringVar(&o.KCPSigningKeyFile, "kcp-signing-key-file", o.KCPSigningKeyFile, "Location of private key file used to sign certificates in kcp.")
}

// Run starts all of controllers for kcp-ocm integration
func (o *ManagerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kcpKubeConfig, err := clientcmd.BuildConfigFromFlags("", o.KCPKubeConfigFile)
	if err != nil {
		return err
	}

	hubKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	kcpDynamicClient, err := dynamic.NewForConfig(kcpKubeConfig)
	if err != nil {
		return err
	}

	kcpDynamicInformer := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		kcpDynamicClient,
		10*time.Minute,
		metav1.NamespaceAll,
		func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = "kcp-integration.open-cluster-management.io/hub=true"
		},
	)

	workspaceController := workspace.NewWorkspaceController(
		o.KCPSigningCertFile,
		o.KCPSigningKeyFile,
		kcpKubeConfig,
		hubKubeClient,
		kcpDynamicInformer.ForResource(helpers.ClusterWorkspaceGVR),
		controllerContext.EventRecorder,
	)

	go kcpDynamicInformer.Start(ctx.Done())

	go workspaceController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
