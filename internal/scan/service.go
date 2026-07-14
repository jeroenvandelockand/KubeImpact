package scan

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/engine"
	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

type Collector func(context.Context, []models.APIResourceSelector) (*collector.Snapshot, error)
type SourceScanner func(context.Context, []models.SourceSpec) (*collector.Snapshot, error)
type Analyzer func(context.Context, *collector.Snapshot, string, policy.Config) (*models.Report, error)

type Service struct {
	collect     Collector
	scanSources SourceScanner
	analyze     Analyzer
	policy      policy.Config
}

func New(config policy.Config, scanSources SourceScanner) *Service {
	return NewWithDependencies(collector.Collect, scanSources, func(ctx context.Context, snapshot *collector.Snapshot, target string, config policy.Config) (*models.Report, error) {
		return engine.Analyze(ctx, snapshot, target, config)
	}, config)
}

func NewWithDependencies(collect Collector, scanSources SourceScanner, analyze Analyzer, config policy.Config) *Service {
	return &Service{collect: collect, scanSources: scanSources, analyze: analyze, policy: config}
}

func (s *Service) Run(ctx context.Context, request models.ScanRequest) (*models.Report, error) {
	targetVersion := knowledge.NormalizeVersion(request.TargetVersion)
	selectors, err := knowledge.ResourceSelectorsThrough(targetVersion)
	if err != nil {
		return nil, err
	}
	if !request.ClusterEnabled() && len(request.Sources) == 0 {
		return nil, errors.New("scan must include the cluster or at least one manifest source")
	}

	snapshot := emptySnapshot()
	if request.ClusterEnabled() {
		clusterSnapshot, collectErr := s.collect(ctx, selectors)
		if collectErr != nil {
			return nil, collectErr
		}
		collector.Merge(snapshot, clusterSnapshot)
		snapshot.ClusterVersion = clusterSnapshot.ClusterVersion
		snapshot.SourceResults = append(snapshot.SourceResults, models.SourceResult{
			Type: models.SourceCluster, Location: "connected-cluster", Resources: snapshotResourceCount(clusterSnapshot), Warnings: append([]string{}, clusterSnapshot.Warnings...),
		})
	}

	if len(request.Sources) > 0 {
		if s.scanSources == nil {
			return nil, errors.New("manifest source scanning is not configured")
		}
		sourceSnapshot, scanErr := s.scanSources(ctx, request.Sources)
		if scanErr != nil {
			return nil, scanErr
		}
		collector.Merge(snapshot, sourceSnapshot)
	}

	if snapshot.ClusterVersion == "" {
		if request.CurrentVersion == "" {
			return nil, errors.New("currentVersion is required for a manifest-only scan")
		}
		snapshot.ClusterVersion = knowledge.NormalizeVersion(request.CurrentVersion)
	} else if request.CurrentVersion != "" && knowledge.NormalizeVersion(request.CurrentVersion) != knowledge.NormalizeVersion(snapshot.ClusterVersion) {
		snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf("Ignored requested currentVersion %s because the connected cluster reports %s.", knowledge.NormalizeVersion(request.CurrentVersion), knowledge.NormalizeVersion(snapshot.ClusterVersion)))
	}

	return s.analyze(ctx, snapshot, targetVersion, s.policy)
}

func emptySnapshot() *collector.Snapshot {
	return &collector.Snapshot{
		Deployments: []appsv1.Deployment{}, StatefulSets: []appsv1.StatefulSet{}, DaemonSets: []appsv1.DaemonSet{}, Services: []corev1.Service{}, Namespaces: []corev1.Namespace{},
		Resources: []models.KubernetesResource{}, DeprecatedAPIRequests: []models.DeprecatedAPIRequest{}, Sources: map[string]string{}, SourceResults: []models.SourceResult{}, Warnings: []string{},
	}
}

func snapshotResourceCount(snapshot *collector.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Deployments) + len(snapshot.StatefulSets) + len(snapshot.DaemonSets) + len(snapshot.Services) + len(snapshot.Resources)
}
