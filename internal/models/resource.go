package models

type KubernetesResource struct {
	Kind                string   `json:"kind"`
	Namespace           string   `json:"namespace"`
	Name                string   `json:"name"`
	Namespaced          bool     `json:"namespaced"`
	ObservedAPIVersions []string `json:"observedApiVersions"`
	Source              string   `json:"source,omitempty"`
}

type APIResourceSelector struct {
	GroupVersion string
	Kind         string
}

type DeprecatedAPIRequest struct {
	GroupVersion   string `json:"groupVersion"`
	Resource       string `json:"resource"`
	Subresource    string `json:"subresource,omitempty"`
	RemovedRelease string `json:"removedRelease"`
}
