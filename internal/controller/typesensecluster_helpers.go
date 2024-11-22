package controller

import (
	"fmt"
	tsv1alpha1 "github.com/akyriako/typesense-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getLabels(ts *tsv1alpha1.TypesenseCluster) map[string]string {
	return map[string]string{
		"app": fmt.Sprintf("%s-sts", *ts.Status.ClusterId),
	}
}

func getObjectMeta(ts *tsv1alpha1.TypesenseCluster, name *string) metav1.ObjectMeta {
	if name == nil {
		name = &ts.Name
	}

	return metav1.ObjectMeta{
		Name:      *name,
		Namespace: ts.Namespace,
		Labels:    getLabels(ts),
	}
}
