package controller

import (
	"context"
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TypesenseClusterReconciler) ReconcileServices(ctx context.Context, ts tsv1alpha1.TypesenseCluster) (*v1.Service, error) {
	headlessSvcName := fmt.Sprintf("%s-sts-svc", ts.Name)
	headlessExists := true
	headlessObjectKey := client.ObjectKey{Namespace: ts.Namespace, Name: headlessSvcName}
	discoSvcName := fmt.Sprintf("%s-svc", ts.Name)
	discoExists := true
	discoObjectKey := client.ObjectKey{Namespace: ts.Namespace, Name: discoSvcName}

	var headless v1.Service
	if err := r.Get(ctx, headlessObjectKey, &headless); err != nil {
		if apierrors.IsNotFound(err) {
			headlessExists = false
		} else {
			r.logger.Error(err, fmt.Sprintf("unable to fetch service: %s", headlessSvcName))
		}
	}

	var disco v1.Service
	if err := r.Get(ctx, discoObjectKey, &disco); err != nil {
		if apierrors.IsNotFound(err) {
			discoExists = false
		} else {
			r.logger.Error(err, fmt.Sprintf("unable to fetch service: %s", discoSvcName))
		}
	}

	if !headlessExists {
		r.logger.Info("creating headless service", "service", headlessObjectKey.Name)

		headless, err := r.createHeadlessService(ctx, headlessObjectKey, &ts)
		if err != nil {
			r.logger.Error(err, "creating headless service failed", "service", headlessObjectKey.Name)
			return nil, err
		}

		return headless, nil
	}

	if !discoExists {
		r.logger.Info("creating resolver service", "service", discoObjectKey.Name)

		headless, err := r.createDiscoService(ctx, discoObjectKey, &ts)
		if err != nil {
			r.logger.Error(err, "creating resolver service failed", "service", discoObjectKey.Name)
			return nil, err
		}

		return headless, nil
	}

	return &headless, nil
}

func (r *TypesenseClusterReconciler) createHeadlessService(ctx context.Context, key client.ObjectKey, ts *tsv1alpha1.TypesenseCluster) (*v1.Service, error) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: v1.ServiceSpec{
			Type:                     v1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector: map[string]string{
				"app": fmt.Sprintf("%s-sts", ts.Name),
			},
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       ts.Spec.ApiPort,
					TargetPort: intstr.IntOrString{IntVal: 8108},
				},
			},
		},
	}

	err := ctrl.SetControllerReference(ts, svc, r.Scheme)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, svc)
	if err != nil {
		return nil, err
	}

	return svc, nil
}

func (r *TypesenseClusterReconciler) createDiscoService(ctx context.Context, key client.ObjectKey, ts *tsv1alpha1.TypesenseCluster) (*v1.Service, error) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": fmt.Sprintf("%s-sts-resolver", ts.Name),
			},
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       ts.Spec.ApiPort,
					TargetPort: intstr.IntOrString{IntVal: 8108},
				},
			},
		},
	}

	err := ctrl.SetControllerReference(ts, svc, r.Scheme)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, svc)
	if err != nil {
		return nil, err
	}

	return svc, nil
}
