package knowledge

type KubernetesRules struct {
	Version        string          `yaml:"version"`
	RemovedAPIs    []APIRule       `yaml:"removedAPIs"`
	DeprecatedAPIs []APIRule       `yaml:"deprecatedAPIs"`
	ResourceChecks []ResourceCheck `yaml:"resourceChecks"`
}

type APIRule struct {
	ID               string `yaml:"id"`
	GroupVersion     string `yaml:"groupVersion"`
	Kind             string `yaml:"kind"`
	RemovedIn        string `yaml:"removedIn"`
	Message          string `yaml:"message"`
	Recommendation   string `yaml:"recommendation"`
	DocumentationURL string `yaml:"documentationURL"`
}

type ResourceCheck struct {
	ID               string `yaml:"id"`
	Name             string `yaml:"name"`
	Severity         string `yaml:"severity"`
	Message          string `yaml:"message"`
	Recommendation   string `yaml:"recommendation"`
	DocumentationURL string `yaml:"documentationURL"`
}
