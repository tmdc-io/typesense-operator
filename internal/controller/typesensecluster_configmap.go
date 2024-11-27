package controller

import (
	"context"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (r *TypesenseClusterReconciler) ReconcileConfigMap(ctx context.Context, ts tsv1alpha1.TypesenseCluster) (updated *bool, err error) {
	configMapName := fmt.Sprintf("%s-nodeslist", ts.Name)
	configMapExists := true
	configMapObjectKey := client.ObjectKey{Namespace: ts.Namespace, Name: configMapName}

	var cm = &v1.ConfigMap{}
	if err = r.Get(ctx, configMapObjectKey, cm); err != nil {
		if apierrors.IsNotFound(err) {
			configMapExists = false
		} else {
			r.logger.Error(err, fmt.Sprintf("unable to fetch config map: %s", configMapName))
		}
	}

	if !configMapExists {
		r.logger.Info("creating config map", "configmap", configMapObjectKey.Name)

		cm, err = r.createConfigMap(ctx, configMapObjectKey, &ts)
		if err != nil {
			r.logger.Error(err, "creating config map failed", "configmap", configMapObjectKey.Name)
			return nil, err
		}
	} else {
		cm, err = r.updateConfigMap(ctx, &ts, cm, nil)
		if err != nil {
			return nil, err
		}
	}

	nodes := strings.Split(cm.Data["nodes"], ",")
	for i := 0; i < len(nodes); i++ {
		nodes[i] = strings.Replace(nodes[i], fmt.Sprintf(":%d:%d", ts.Spec.PeeringPort, ts.Spec.ApiPort), "", 1)
	}
	return &configMapExists, nil
}

const nodeNameLenLimit = 64

func (r *TypesenseClusterReconciler) createConfigMap(ctx context.Context, key client.ObjectKey, ts *tsv1alpha1.TypesenseCluster) (*v1.ConfigMap, error) {
	nodes, err := r.getNodes(ts, ts.Spec.Replicas)
	if err != nil {
		return nil, err
	}

	cm := &v1.ConfigMap{
		ObjectMeta: getObjectMeta(ts, &key.Name),
		Data: map[string]string{
			"nodes": strings.Join(nodes, ","),
		},
	}

	err = ctrl.SetControllerReference(ts, cm, r.Scheme)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, cm)
	if err != nil {
		return nil, err
	}

	return cm, nil
}

func (r *TypesenseClusterReconciler) updateConfigMap(ctx context.Context, ts *tsv1alpha1.TypesenseCluster, cm *v1.ConfigMap, replicas *int32) (*v1.ConfigMap, error) {
	//nodes := make([]string, 0)
	//pods, err := r.getPods(ctx, ts)
	//if err != nil {
	//	return nil, err
	//}
	//
	//desired := cm.DeepCopy()
	//
	//for _, pod := range pods.Items {
	//	for _, container := range pod.Spec.Containers {
	//		if container.Name == "typesense" && strings.TrimSpace(pod.Status.PodIP) != "" && pod.Status.ContainerStatuses[0].Ready {
	//			nodes = append(nodes, fmt.Sprintf("%s:%d:%d", pod.Status.PodIP, ts.Spec.PeeringPort, ts.Spec.ApiPort))
	//		}
	//	}
	//}

	stsName := fmt.Sprintf("%s-sts", ts.Name)
	stsObjectKey := client.ObjectKey{
		Name:      stsName,
		Namespace: ts.Namespace,
	}

	var sts = &appsv1.StatefulSet{}
	if err := r.Get(ctx, stsObjectKey, sts); err != nil {
		r.logger.Error(err, fmt.Sprintf("unable to fetch statefulset: %s", stsName))
	}

	if replicas == nil {
		replicas = sts.Spec.Replicas
	}

	nodes, err := r.getNodes(ts, *replicas)
	if err != nil {
		return nil, err
	}

	availableNodes := len(nodes)
	if availableNodes == 0 {
		r.logger.Info("empty quorum configuration")
		return nil, fmt.Errorf("empty quorum configuration")
	}

	desired := cm.DeepCopy()
	desired.Data = map[string]string{
		"nodes": strings.Join(nodes, ","),
	}

	r.logger.Info("quorum configuration", "nodes", availableNodes, "nodes", nodes)

	if cm.Data["nodes"] != desired.Data["nodes"] {
		r.logger.Info("updating quorum configuration")

		err := r.Update(ctx, desired)
		if err != nil {
			r.logger.Error(err, "updating quorum configuration failed")
			return nil, err
		}
	}

	return desired, nil
}

func (r *TypesenseClusterReconciler) getPods(ctx context.Context, ts *tsv1alpha1.TypesenseCluster) (*v1.PodList, error) {
	listOptions := []client.ListOption{
		client.InNamespace(ts.Namespace),
		client.MatchingLabels(getLabels(ts)),
	}

	pods := &v1.PodList{}
	err := r.List(ctx, pods, listOptions...)
	if err != nil {
		r.logger.Error(err, "failed to list quorum pods")
		return nil, err
	}

	if len(pods.Items) == 0 {
		r.logger.Info("no pods found in quorum")
		return nil, fmt.Errorf("no pods found in quorum")
	}

	return pods, nil
}

func (r *TypesenseClusterReconciler) getNodes(ts *tsv1alpha1.TypesenseCluster, replicas int32) ([]string, error) {
	nodes := make([]string, replicas)
	for i := 0; i < int(replicas); i++ {
		nodeName := fmt.Sprintf("%s-sts-%d.%s-sts-svc.%s.svc.cluster.local", ts.Name, i, ts.Name, ts.Namespace)
		if len(nodeName) > nodeNameLenLimit {
			return nil, fmt.Errorf("raft error: node name should not exceed %d characters: %s", nodeNameLenLimit, nodeName)
		}

		nodes[i] = fmt.Sprintf("%s:%d:%d", nodeName, ts.Spec.PeeringPort, ts.Spec.ApiPort)
	}

	return nodes, nil
}
