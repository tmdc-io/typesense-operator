package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	"strings"
)

func (s *TypesenseClusterSpec) IsCorsEnabled() bool {
	if s.CorsDomains != nil && strings.TrimSpace(*s.CorsDomains) != "" {
		return true
	}
	return false
}

func (s *TypesenseClusterSpec) GetCorsDomains() string {
	if s.IsCorsEnabled() {
		return *s.CorsDomains
	}
	return ""
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
