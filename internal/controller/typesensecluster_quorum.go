package controller

import (
	"context"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (r *TypesenseClusterReconciler) ReconcileRaftQuorum(ctx context.Context, ts tsv1alpha1.TypesenseCluster, cm v1.ConfigMap) error {
	r.logger.Info("reconciling quorum")

	listOptions := []client.ListOption{
		client.InNamespace(ts.Namespace),
		client.MatchingLabels(getLabels(&ts)),
	}

	pods := &v1.PodList{}
	err := r.List(ctx, pods, listOptions...)
	if err != nil {
		r.logger.Error(err, "failed to list quorum pods")
		return err
	}

	if len(pods.Items) == 0 {
		r.logger.Info("no pods found in quorum")
		return nil
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

	if len(nodes) == 0 {
		r.logger.Info("empty quorum configuration")
		return nil
	}

	desired.Data = map[string]string{
		"nodes": strings.Join(nodes, ","),
	}

	r.logger.Info("quorum configuration", "nodes", len(nodes), "nodes", nodes)

	if cm.Data["nodes"] != desired.Data["nodes"] {
		r.logger.Info("updating quorum configuration")

		err := r.Update(ctx, desired)
		if err != nil {
			r.logger.Error(err, "reconciling raft quorum failed")
			return err
		}

	}

	return nil
}
