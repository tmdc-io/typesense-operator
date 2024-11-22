package controller

import (
	"context"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (r *TypesenseClusterReconciler) ReconcileConfigMap(ctx context.Context, ts tsv1alpha1.TypesenseCluster) (*v1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s-nodeslist", ts.Name)
	configMapExists := true
	configMapObjectKey := client.ObjectKey{Namespace: ts.Namespace, Name: configMapName}

	var nodesList v1.ConfigMap
	if err := r.Get(ctx, configMapObjectKey, &nodesList); err != nil {
		if apierrors.IsNotFound(err) {
			configMapExists = false
		} else {
			r.logger.Error(err, fmt.Sprintf("unable to config map: %s", configMapName))
		}
	}

	if !configMapExists {
		r.logger.Info("creating config map", "configmap", configMapObjectKey.Name)

		nodesList, err := r.createConfigMap(ctx, configMapObjectKey, &ts)
		if err != nil {
			r.logger.Error(err, "creating config map failed", "configmap", configMapObjectKey.Name)
			return nil, err
		}

		return nodesList, nil
	}

	return &nodesList, nil
}

func (r *TypesenseClusterReconciler) createConfigMap(ctx context.Context, key client.ObjectKey, ts *tsv1alpha1.TypesenseCluster) (*v1.ConfigMap, error) {
	nodes := make([]string, ts.Spec.Replicas)
	for i := 0; i < int(ts.Spec.Replicas); i++ {
		nodes[i] = fmt.Sprintf("%s-ts-%d.%s-sts-svc.%s.svc.cluster.local:%d:%d", ts.Name, i, ts.Name, ts.Namespace, ts.Spec.PeeringPort, ts.Spec.ApiPort)
	}

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels: map[string]string{
				"app": fmt.Sprintf("%s-sts", ts.Name),
			},
		},
		Data: map[string]string{
			"nodes": strings.Join(nodes, ","),
		},
	}

	err := ctrl.SetControllerReference(ts, cm, r.Scheme)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, cm)
	if err != nil {
		return nil, err
	}

	return cm, nil
}
