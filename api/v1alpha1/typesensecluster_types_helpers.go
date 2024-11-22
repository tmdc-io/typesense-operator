package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

func (s *TypesenseClusterSpec) GetCors() *CorsSpec {
	if s.Cors != nil {
		return s.Cors
	}

	return &CorsSpec{
		Enabled: false,
		Domains: "",
	}
}

func (s *TypesenseClusterSpec) GetStorage() *StorageSpec {
	if s.Storage != nil {
		return s.Storage
	}

	return &StorageSpec{
		Size:             resource.MustParse("100Mi"),
		StorageClassName: "standard",
	}
}
