package addons

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"

	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	policyv1beta1 "open-cluster-management.io/governance-policy-propagator/api/v1beta1"
	msav1alpha1 "open-cluster-management.io/managed-serviceaccount/api/v1alpha1"
	placementrulev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"

	"github.com/skeeey/kcp-integration/pkg/controllers/workspace/addons/managedsa"
	"github.com/skeeey/kcp-integration/pkg/controllers/workspace/addons/policy"
	"github.com/skeeey/kcp-integration/pkg/controllers/workspace/addons/worker"
	"github.com/skeeey/kcp-integration/pkg/helpers"

	clusterinfov1beta1 "github.com/stolostron/cluster-lifecycle-api/clusterinfo/v1beta1"

	ctrl "sigs.k8s.io/controller-runtime"
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

func StartAddOnManagers(ctx context.Context, addOnCtx *helpers.AddOnManagerContext) {
	ctrl.SetLogger(klogr.New())

	addonManager, err := addonmanager.New(addOnCtx.CtrlContext.KubeConfig)
	if err != nil {
		klog.Errorf("unable to create addon manager %v", err)
		return
	}

	ctrlManager, err := ctrl.NewManager(addOnCtx.CtrlContext.KubeConfig, ctrl.Options{Scheme: scheme})
	if err != nil {
		klog.Errorf("unable to create controller-runtime manager %v", err)
		return
	}

	// add work addon manager
	if err := worker.AddAddon(addOnCtx, addonManager, ctrlManager); err != nil {
		klog.Errorf("unable to add work addon manager %v", err)
		return
	}

	// add managed-serviceaccount addon manager
	if err := managedsa.AddAddon(addOnCtx, addonManager, ctrlManager); err != nil {
		klog.Errorf("unable to add managed-serviceaccount addon manager %v", err)
		return
	}

	// add policy addon manager
	if err := policy.AddAddon(addOnCtx, addonManager, ctrlManager); err != nil {
		klog.Errorf("unable to add policy addon manager %v", err)
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
