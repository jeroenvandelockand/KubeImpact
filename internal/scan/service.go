package scan

import (
	"context"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/engine"
	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
)

type Collector func(context.Context, []models.APIResourceSelector) (*collector.Snapshot, error)
type Analyzer func(context.Context, *collector.Snapshot, string) (*models.Report, error)

type Service struct {
	collect Collector
	analyze Analyzer
}

func New() *Service {
	return NewWithDependencies(collector.Collect, engine.Analyze)
}

func NewWithDependencies(collect Collector, analyze Analyzer) *Service {
	return &Service{collect: collect, analyze: analyze}
}

func (s *Service) Run(ctx context.Context, targetVersion string) (*models.Report, error) {
	selectors, err := knowledge.ResourceSelectorsThrough(targetVersion)
	if err != nil {
		return nil, err
	}

	snapshot, err := s.collect(ctx, selectors)
	if err != nil {
		return nil, err
	}
	return s.analyze(ctx, snapshot, knowledge.NormalizeVersion(targetVersion))
}
