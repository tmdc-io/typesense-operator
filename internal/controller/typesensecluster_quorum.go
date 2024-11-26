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
	"k8s.io/utils/ptr"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"
)

type NodeHealthResponse struct {
	Ok            bool   `json:"ok"`
	ResourceError string `json:"resource_error"`
}

func (r *TypesenseClusterReconciler) ReconcileQuorum(ctx context.Context, ts tsv1alpha1.TypesenseCluster, sts appsv1.StatefulSet) (ConditionQuorum, error) {
	r.logger.Info("reconciling quorum")

	if sts.Status.ReadyReplicas != sts.Status.Replicas {
		return ConditionReasonStatefulSetNotReady, fmt.Errorf("statefulset not ready: %d/%d replicas ready", sts.Status.ReadyReplicas, sts.Status.Replicas)
	}

	condition, err := r.getQuorumHealth(ctx, &ts, &sts)
	return condition, err
}

func (r *TypesenseClusterReconciler) getQuorumHealth(ctx context.Context, ts *tsv1alpha1.TypesenseCluster, sts *appsv1.StatefulSet) (ConditionQuorum, error) {
	configMapName := fmt.Sprintf("%s-nodeslist", ts.Name)
	configMapObjectKey := client.ObjectKey{Namespace: ts.Namespace, Name: configMapName}

	var cm = &v1.ConfigMap{}
	if err := r.Get(ctx, configMapObjectKey, cm); err != nil {
		r.logger.Error(err, fmt.Sprintf("unable to fetch config map: %s", configMapName))
	}

	nodes := strings.Split(cm.Data["nodes"], ",")
	availableNodes := len(nodes)
	minRequiredNodes := (availableNodes-1)/2 + 1
	if availableNodes < minRequiredNodes {
		return ConditionReasonQuorumNotReady, fmt.Errorf("quorum has less than minimum %d available nodes", minRequiredNodes)
	}

	healthResults := make(map[string]bool, availableNodes)
	httpClient := &http.Client{
		Timeout: 500 * time.Millisecond,
	}

	for _, node := range nodes {
		tsUrl := strings.Replace(node, fmt.Sprintf(":%d", ts.Spec.PeeringPort), "", 1)
		resp, err := httpClient.Get(fmt.Sprintf("http://%s/health", tsUrl))
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
	}

	if healthyNodes < minRequiredNodes {
		if sts.Status.ReadyReplicas > 1 {
			r.logger.Info("downgrading quorum")

			_, err := r.updateConfigMap(ctx, ts, cm, ptr.To[int32](1))
			if err != nil {
				return ConditionReasonQuorumNotReady, err
			}

			err = r.scaleStatefulSet(ctx, sts, 1)
			if err != nil {
				return ConditionReasonQuorumNotReady, err
			}

			return ConditionReasonQuorumDowngraded, nil
		}

		return ConditionReasonQuorumNotReady, fmt.Errorf("quorum has %d healthy nodes, minimum required %d", healthyNodes, minRequiredNodes)
	} else {
		if sts.Status.ReadyReplicas < ts.Spec.Replicas {
			r.logger.Info("upgrading quorum")

			_, err := r.updateConfigMap(ctx, ts, cm, &ts.Spec.Replicas)
			if err != nil {
				return ConditionReasonQuorumNotReady, err
			}

			err = r.scaleStatefulSet(ctx, sts, ts.Spec.Replicas)
			if err != nil {
				return ConditionReasonQuorumNotReady, err
			}

			return ConditionReasonQuorumUpgraded, nil
		}
	}

	return ConditionReasonQuorumReady, nil
}

func (r *TypesenseClusterReconciler) scaleStatefulSet(ctx context.Context, sts *appsv1.StatefulSet, desiredReplicas int32) error {
	if sts.Spec.Replicas != nil && *sts.Spec.Replicas == desiredReplicas {
		r.logger.Info("statefulset already scaled to desired replicas", "name", sts.Name, "replicas", desiredReplicas)
		return nil
	}

	desired := sts.DeepCopy()
	desired.Spec.Replicas = &desiredReplicas
	if err := r.Client.Update(ctx, desired); err != nil {
		r.logger.Error(err, "updating stateful replicas failed", "name", desired.Name)
		return err
	}

	return nil
}
