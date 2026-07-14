package models

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Low      Severity = "low"
	Info     Severity = "info"
)

func ValidSeverity(severity Severity) bool {
	switch severity {
	case Critical, High, Medium, Low, Info:
		return true
	default:
		return false
	}
}

type Finding struct {
	ID          string   `json:"id"`
	Fingerprint string   `json:"fingerprint"`
	Analyzer    string   `json:"analyzer"`
	Severity    Severity `json:"severity"`

	Category string `json:"category"`

	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	FieldPath string `json:"fieldPath,omitempty"`

	CurrentValue  string `json:"currentValue,omitempty"`
	ExpectedValue string `json:"expectedValue,omitempty"`

	Message          string `json:"message"`
	Recommendation   string `json:"recommendation,omitempty"`
	DocumentationURL string `json:"documentationUrl,omitempty"`
}
