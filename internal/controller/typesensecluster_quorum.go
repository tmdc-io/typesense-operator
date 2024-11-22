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
		for containerIdx, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if int(port.ContainerPort) == ts.Spec.ApiPort {
					ready := false
					if containerIdx <= len(pod.Status.ContainerStatuses)-1 {
						ready = pod.Status.ContainerStatuses[containerIdx].Ready
					}

					if strings.TrimSpace(pod.Status.PodIP) != "" && ready {
						nodes = append(nodes, fmt.Sprintf("%s:%d:%d", pod.Status.PodIP, ts.Spec.PeeringPort, port.ContainerPort))
					}
				}
			}
		}

		if len(nodes) == 0 {
			{
				r.logger.Info("empty quorum configuration")
			}

			desired.Data = map[string]string{
				"nodes": strings.Join(nodes, ","),
			}
		}

		if cm.Data["nodes"] != desired.Data["nodes"] {
			r.logger.Info("updating quorum configuration")

			err := r.Update(ctx, desired)
			if err != nil {
				r.logger.Error(err, "reconciling raft quorum failed")
				return err
			}

		}
	}

	r.logger.Info("quorum configuration", "nodes", len(nodes), "nodes", nodes)
	return nil
}
