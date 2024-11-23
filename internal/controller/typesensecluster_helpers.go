package controller

import (
	"context"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TypesenseClusterReconciler) updatePatch(ctx context.Context, obj *tsv1alpha1.TypesenseCluster, patch client.Patch) error {
	err := r.Status().Patch(ctx, obj, patch)
	if err != nil {
		r.logger.Error(err, "unable to patch typesense cluster status")
		return err
	}

	return nil
}

func (r *TypesenseClusterReconciler) UpdateStatus(ctx context.Context, ts *tsv1alpha1.TypesenseCluster, healthy, ready bool) error {
	patch := client.MergeFrom(ts.DeepCopy())
	ts.Status.Healthy = healthy
	ts.Status.Ready = ready

	err := r.Status().Patch(ctx, ts, patch)
	if err != nil {
		r.logger.Error(err, "unable to patch typesense cluster status")
		return err
	}

	return nil
}
