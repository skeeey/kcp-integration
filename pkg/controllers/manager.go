package controllers

import (
	"context"
	"time"

	"github.com/skeeey/kcp-integration/pkg/controllers/clustermanager"
	"github.com/skeeey/kcp-integration/pkg/controllers/workspace"
	"github.com/skeeey/kcp-integration/pkg/helpers"
	"github.com/spf13/pflag"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

// ManagerOptions defines the flags for kcp-ocm integration controller manager
type ManagerOptions struct {
	KCPKubeConfigFile       string
	KCPSigningCertFile      string
	KCPSigningKeyFile       string
	RegistrationWebhookHost string
	WorkWebhookHost         string
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
	flags.StringVar(&o.RegistrationWebhookHost, "registration-webhook-host", o.RegistrationWebhookHost, "Host of registration webhook.")
	flags.StringVar(&o.WorkWebhookHost, "work-webhook-host", o.WorkWebhookHost, "Host of work webhook host.")
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

	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)
	kcpDynamicInformer := dynamicinformer.NewDynamicSharedInformerFactory(kcpDynamicClient, 10*time.Minute)

	clusterManagerController := clustermanager.NewClusterManagerController(
		kcpKubeConfig,
		hubKubeClient,
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		o.RegistrationWebhookHost,
		o.WorkWebhookHost,
		controllerContext.EventRecorder,
	)

	workspaceController := workspace.NewOrgWorkspaceController(
		o.KCPSigningCertFile,
		o.KCPSigningKeyFile,
		kcpKubeConfig,
		hubKubeClient,
		operatorClient.OperatorV1().ClusterManagers(),
		kcpDynamicInformer.ForResource(helpers.ClusterWorkspaceGVR),
		controllerContext.EventRecorder,
	)

	go operatorInformer.Start(ctx.Done())
	go kcpDynamicInformer.Start(ctx.Done())

	go clusterManagerController.Run(ctx, 1)
	go workspaceController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
