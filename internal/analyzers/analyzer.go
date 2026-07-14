package analyzers

import (
	"context"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

type Analyzer interface {
	Name() string

	Analyze(
		ctx context.Context,
		snapshot *collector.Snapshot,
	) (*models.AnalysisResult, error)
}
