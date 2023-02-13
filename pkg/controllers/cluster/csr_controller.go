package cluster

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/klog/v2"
)

const (
	spokeClusterNameLabel = "open-cluster-management.io/cluster-name"
)

// clusterAutoApproveController approve the csr of an accepted spoke cluster on the hub.
type clusterAutoApproveController struct {
	kubeClient    kubernetes.Interface
	clusterClient clusterclient.Interface
	csrLister     certificateslisters.CertificateSigningRequestLister
}

// NewClusterAutoApproveController creates a new csr approving controller
func NewClusterAutoApproveController(
	kubeClient kubernetes.Interface,
	clusterClient clusterclient.Interface,
	csrInformer certificatesinformers.CertificateSigningRequestInformer,
	recorder events.Recorder) factory.Controller {
	c := &clusterAutoApproveController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		csrLister:     csrInformer.Lister(),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer.Informer()).
		WithSync(c.sync).
		ToController("cluster-auto-approve-controller", recorder)
}

func (c *clusterAutoApproveController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()

	klog.Infof("Reconciling CertificateSigningRequests %q", csrName)

	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		klog.Infof("CertificateSigningRequests %q not found", csrName)
		return nil
	}
	if err != nil {
		return err
	}

	if isCSRInTerminalState(&csr.Status) {
		klog.Infof("CertificateSigningRequests %q is in terminal", csrName)
		return nil
	}

	clusterName, ok := csr.Labels[spokeClusterNameLabel]
	if !ok {
		klog.Infof("CertificateSigningRequests %q has no cluster label", csrName)
		return nil
	}

	_, err = c.clusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// no managed cluster, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// TODO do more checks for the csr

	csr = csr.DeepCopy()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Status:         corev1.ConditionTrue,
		Reason:         "AutoApprovedByCSRController",
		Message:        "The cluster-auto-approve-controller automatically approved this CSR",
		LastUpdateTime: metav1.Now(),
	})
	if _, err := c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(
		ctx, csr.Name, csr, metav1.UpdateOptions{}); err != nil {
		return err
	}

	syncCtx.Recorder().Eventf(
		"ManagedClusterCSRAutoApproved", "managed cluster csr %q is auto approved", csr.Name)
	return nil
}

// check whether a CSR is in terminal state
func isCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}
