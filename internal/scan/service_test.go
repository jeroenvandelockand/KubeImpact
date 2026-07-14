package scan

import (
	"context"
	"testing"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

func TestRunBuildsSelectorsBeforeCollection(t *testing.T) {
	var collected []models.APIResourceSelector
	service := NewWithDependencies(
		func(_ context.Context, selectors []models.APIResourceSelector) (*collector.Snapshot, error) {
			collected = selectors
			return &collector.Snapshot{ClusterVersion: "v1.34.1"}, nil
		},
		func(_ context.Context, _ *collector.Snapshot, target string) (*models.Report, error) {
			return &models.Report{TargetVersion: target}, nil
		},
	)

	report, err := service.Run(context.Background(), "v1.36.2")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TargetVersion != "1.36" || len(collected) != 1 {
		t.Fatalf("Run() report/selectors = %#v / %#v", report, collected)
	}
}
