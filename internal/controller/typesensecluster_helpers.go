package controller

import (
	"context"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (r *TypesenseClusterReconciler) updateClusterId(ctx context.Context, ts *tsv1alpha1.TypesenseCluster) error {
	patch := client.MergeFrom(ts.DeepCopy())

	clusterId, err := generateSecureRandomString(4)
	if err != nil {
		return err
	}
	ts.Status.ClusterId = func(s string) *string {
		l := fmt.Sprint("tsc-", strings.ToLower(s))
		return &l
	}(clusterId)

	return r.updatePatch(ctx, ts, patch)
}

func (r *TypesenseClusterReconciler) updatePatch(ctx context.Context, obj *tsv1alpha1.TypesenseCluster, patch client.Patch) error {
	err := r.Status().Patch(ctx, obj, patch)
	if err != nil {
		r.logger.Error(err, "unable to patch typesense cluster status")
		return err
	}

	return nil
}
