package controller

import (
	"context"
	"encoding/json"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	"github.com/pkg/errors"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"
)

type NodeHealthResponse struct {
	Ok            bool   `json:"ok"`
	ResourceError string `json:"resource_error"`
}

func (r *TypesenseClusterReconciler) ReconcileQuorum(ctx context.Context, ts tsv1alpha1.TypesenseCluster, cm v1.ConfigMap, sts appsv1.StatefulSet) (bool, error) {
	r.logger.Info("reconciling quorum")

	listOptions := []client.ListOption{
		client.InNamespace(ts.Namespace),
		client.MatchingLabels(getLabels(&ts)),
	}

	pods := &v1.PodList{}
	err := r.List(ctx, pods, listOptions...)
	if err != nil {
		r.logger.Error(err, "failed to list quorum pods")
		return false, err
	}

	if len(pods.Items) == 0 {
		r.logger.Info("no pods found in quorum")
		return false, nil
	}

	desired := cm.DeepCopy()
	var nodes []string

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == "typesense" && strings.TrimSpace(pod.Status.PodIP) != "" && pod.Status.ContainerStatuses[0].Ready {
				nodes = append(nodes, fmt.Sprintf("%s:%d:%d", pod.Status.PodIP, ts.Spec.PeeringPort, ts.Spec.ApiPort))
			}
		}
	}

	availableNodes := len(nodes)
	if availableNodes == 0 {
		r.logger.Info("empty quorum configuration")
		return false, nil
	}

	desired.Data = map[string]string{
		"nodes": strings.Join(nodes, ","),
	}

	r.logger.Info("quorum configuration", "nodes", availableNodes, "nodes", nodes)

	if cm.Data["nodes"] != desired.Data["nodes"] {
		r.logger.Info("updating quorum configuration")

		err := r.Update(ctx, desired)
		if err != nil {
			r.logger.Error(err, "reconciling raft quorum failed")
			return false, err
		}
	}

	ready, err := r.getQuorumHealth(&ts, nodes)
	if err != nil {
		return false, nil
	}

	return ready, nil
}

func (r *TypesenseClusterReconciler) getQuorumHealth(ts *tsv1alpha1.TypesenseCluster, nodes []string) (bool, error) {
	availableNodes := len(nodes)
	minRequiredNodes := (availableNodes - 1) / 2
	if availableNodes < minRequiredNodes {
		return false, nil
	}

	healthResults := make(map[string]bool, availableNodes)
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, node := range nodes {
		node = strings.Replace(node, fmt.Sprintf(":%d", ts.Spec.PeeringPort), "", 1)
		resp, err := httpClient.Get(fmt.Sprintf("http://%s/health", node))
		if err != nil {
			r.logger.Error(err, "health check failed", "node", node)
			healthResults[node] = false
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			r.logger.Error(err, "reading health check response failed", "node", node)
			healthResults[node] = false
			continue
		}

		if resp.StatusCode != http.StatusOK {
			r.logger.Error(err, fmt.Sprintf("health check failed, status code: %d, response: %s", resp.StatusCode, body), "node", node)
			healthResults[node] = false
			continue
		}

		var ready NodeHealthResponse
		err = json.Unmarshal(body, &ready)
		if err != nil {
			r.logger.Error(err, "unmarshalling health check response failed", "node", node)
			healthResults[node] = false
			continue
		}

		if !ready.Ok && ready.ResourceError != "" {
			err := errors.New(ready.ResourceError)
			r.logger.Error(err, "health check reported a node error", "node", node)
		}
		healthResults[node] = ready.Ok
	}

	healthyNodes := availableNodes
	for _, healthy := range healthResults {
		if !healthy {
			healthyNodes--
		}

		if healthyNodes < minRequiredNodes {
			return false, nil
		}
	}

	return true, nil

}
