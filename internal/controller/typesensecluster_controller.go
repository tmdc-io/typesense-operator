/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/typesense/typesense-go/v2/typesense"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"

	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
)

// TypesenseClusterReconciler reconciles a TypesenseCluster object
type TypesenseClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logr.Logger
}

var (
	eventFilters = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// We only need to check generation changes here, because it is only
			// updated on spec changes. On the other hand RevisionVersion
			// changes also on status changes. We want to omit reconciliation
			// for status updates.
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// DeleteStateUnknown evaluates to false only if the object
			// has been confirmed as deleted by the api server.
			return !e.DeleteStateUnknown
		},
	})

	requeueAfter = time.Second * 30
)

// +kubebuilder:rbac:groups=ts.opentelekomcloud.com,resources=typesenseclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ts.opentelekomcloud.com,resources=typesenseclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ts.opentelekomcloud.com,resources=typesenseclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TypesenseCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *TypesenseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.logger = log.Log.WithValues("namespace", req.Namespace, "cluster", req.Name)
	r.logger.Info("reconciling cluster")

	var ts tsv1alpha1.TypesenseCluster
	if err := r.Get(ctx, req.NamespacedName, &ts); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		r.logger.Error(err, "unable to fetch typesense-cluster")
		return ctrl.Result{}, err
	}

	//sa, err := r.ReconcileRbac(ctx, ts)
	//if err != nil {
	//	return ctrl.Result{}, err
	//}

	secret, err := r.ReconcileSecret(ctx, ts)
	if err != nil {
		return ctrl.Result{}, err
	}

	cm, err := r.ReconcileConfigMap(ctx, ts)
	if err != nil {
		return ctrl.Result{}, err
	}

	svc, err := r.ReconcileServices(ctx, ts)
	if err != nil {
		return ctrl.Result{}, err
	}

	sts, err := r.ReconcileStatefulSet(ctx, ts, *secret, *cm, *svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.ReconcileRaftQuorum(ctx, ts, *cm)
	if err != nil {
		return ctrl.Result{}, err
	}

	healthy := false
	if sts.Status.ReadyReplicas == sts.Status.Replicas {
		healthy = true
	}

	apiKey := string(secret.Data[adminApiKeyName])
	tsSvc := fmt.Sprintf("http://%s-svc.%s.svc.cluster.local:%d", ts.Name, ts.Namespace, ts.Spec.ApiPort)
	tsClient := typesense.NewClient(typesense.WithServer(tsSvc), typesense.WithAPIKey(apiKey))

	ready, err := tsClient.Health(ctx, 10*time.Second)
	if err != nil {
		r.logger.Error(err, "health check failed")
		ready = false
	}

	err = r.UpdateStatus(ctx, &ts, healthy, ready)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TypesenseClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tsv1alpha1.TypesenseCluster{}, eventFilters).
		Complete(r)
}
