package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Cluster struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Region   string `json:"region"`
}

func GetAllClusters(url string) ([]Cluster, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get clusters statuscode=%d, status=%s", resp.StatusCode, resp.Status)
	}

	clusters := []Cluster{}
	if err := json.NewDecoder(resp.Body).Decode(&clusters); err != nil {
		return nil, err
	}

	return clusters, nil
}

func GetCluster(url string, clusterID string) (*Cluster, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s", url, clusterID), nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get cluster %s statuscode=%d, status=%s",
			clusterID, resp.StatusCode, resp.Status)
	}

	cluster := &Cluster{}
	if err := json.NewDecoder(resp.Body).Decode(cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

func CreateCluster(server string, managedCluster *clusterv1.ManagedCluster) error {
	clusterData, err := json.Marshal(toCluster(managedCluster))
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/cluster_inventory_mgmt/v1/clusters", server)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(clusterData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create cluster %s statuscode=%d, status=%s",
			managedCluster.Name, resp.StatusCode, resp.Status)
	}

	return nil
}

func toCluster(managedCluster *clusterv1.ManagedCluster) *Cluster {
	id := strings.TrimPrefix(managedCluster.Name, "cluster-")

	status := "Unknown"
	available := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if available != nil {
		switch available.Status {
		case metav1.ConditionTrue:
			status = "Available"
		case metav1.ConditionFalse:
			status = "Unavailable"
		}
	}

	return &Cluster{
		ID:       id,
		Status:   status,
		Type:     findClusterClaims(managedCluster.Status.ClusterClaims, "product.open-cluster-management.io"),
		Version:  findClusterClaims(managedCluster.Status.ClusterClaims, "kubeversion.open-cluster-management.io"),
		Platform: findClusterClaims(managedCluster.Status.ClusterClaims, "platform.open-cluster-management.io"),
		Region:   findClusterClaims(managedCluster.Status.ClusterClaims, "region.open-cluster-management.io"),
	}
}

func findClusterClaims(claims []clusterv1.ManagedClusterClaim, name string) string {
	for _, claim := range claims {
		if claim.Name == name {
			return claim.Value
		}
	}

	return "unknown"
}
