package models

type UpgradeImpact struct {
	Rule        string   `json:"rule"`
	Fingerprint string   `json:"fingerprint"`
	Severity    Severity `json:"severity"`
	Category    string   `json:"category,omitempty"`

	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	FieldPath string `json:"fieldPath,omitempty"`

	CurrentValue  string `json:"currentValue,omitempty"`
	ExpectedValue string `json:"expectedValue,omitempty"`

	Message          string `json:"message"`
	Recommendation   string `json:"recommendation"`
	DocumentationURL string `json:"documentationUrl,omitempty"`
}
