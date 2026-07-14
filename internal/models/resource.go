package models

type KubernetesResource struct {
	Kind                string   `json:"kind"`
	Namespace           string   `json:"namespace"`
	Name                string   `json:"name"`
	Namespaced          bool     `json:"namespaced"`
	ObservedAPIVersions []string `json:"observedApiVersions"`
}

type APIResourceSelector struct {
	GroupVersion string
	Kind         string
}
