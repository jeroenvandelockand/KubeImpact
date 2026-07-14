package models

type ChangeStatus string

const (
	ChangeNew       ChangeStatus = "new"
	ChangeUnchanged ChangeStatus = "unchanged"
	ChangeResolved  ChangeStatus = "resolved"
)

type ReportComparison struct {
	PreviousScanID string           `json:"previousScanId,omitempty"`
	New            int              `json:"new"`
	Unchanged      int              `json:"unchanged"`
	Resolved       int              `json:"resolved"`
	ResolvedItems  []ResolvedSignal `json:"resolvedItems"`
}

type ResolvedSignal struct {
	Fingerprint string       `json:"fingerprint"`
	Rule        string       `json:"rule"`
	Type        string       `json:"type"`
	Severity    Severity     `json:"severity"`
	Namespace   string       `json:"namespace"`
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Container   string       `json:"container,omitempty"`
	Source      string       `json:"source,omitempty"`
	Message     string       `json:"message"`
	Change      ChangeStatus `json:"change"`
}
