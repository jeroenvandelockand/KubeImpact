package models

import "time"

type ScanStatus string

const (
	ScanPending   ScanStatus = "pending"
	ScanRunning   ScanStatus = "running"
	ScanCompleted ScanStatus = "completed"
	ScanFailed    ScanStatus = "failed"
)

type ScanRequest struct {
	TargetVersion  string       `json:"targetVersion"`
	CurrentVersion string       `json:"currentVersion,omitempty"`
	IncludeCluster *bool        `json:"includeCluster,omitempty"`
	Sources        []SourceSpec `json:"sources,omitempty"`
}

func (r ScanRequest) ClusterEnabled() bool {
	return r.IncludeCluster == nil || *r.IncludeCluster
}

type SourceType string

const (
	SourceCluster   SourceType = "cluster"
	SourceDirectory SourceType = "directory"
	SourceHelm      SourceType = "helm"
	SourceGit       SourceType = "git"
)

type SourceSpec struct {
	Type        SourceType `json:"type"`
	Path        string     `json:"path,omitempty"`
	URL         string     `json:"url,omitempty"`
	Ref         string     `json:"ref,omitempty"`
	ChartPath   string     `json:"chartPath,omitempty"`
	ReleaseName string     `json:"releaseName,omitempty"`
	Namespace   string     `json:"namespace,omitempty"`
	ValuesFiles []string   `json:"valuesFiles,omitempty"`
}

type SourceResult struct {
	Type      SourceType `json:"type"`
	Location  string     `json:"location"`
	Documents int        `json:"documents"`
	Resources int        `json:"resources"`
	Warnings  []string   `json:"warnings"`
}

type ScanRecord struct {
	ID          string      `json:"id"`
	Status      ScanStatus  `json:"status"`
	Request     ScanRequest `json:"request"`
	CreatedAt   time.Time   `json:"createdAt"`
	StartedAt   *time.Time  `json:"startedAt,omitempty"`
	CompletedAt *time.Time  `json:"completedAt,omitempty"`
	Error       string      `json:"error,omitempty"`
	Report      *Report     `json:"report,omitempty"`
}
