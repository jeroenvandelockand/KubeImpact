package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
	"kubeimpact/internal/sources"
)

func TestRunBuildsSelectorsAndCombinesSources(t *testing.T) {
	var collected []models.APIResourceSelector
	service := NewWithDependencies(
		func(_ context.Context, selectors []models.APIResourceSelector) (*collector.Snapshot, error) {
			collected = selectors
			return &collector.Snapshot{ClusterVersion: "v1.34.1", Sources: map[string]string{}}, nil
		},
		func(_ context.Context, _ []models.SourceSpec) (*collector.Snapshot, error) {
			return &collector.Snapshot{Warnings: []string{"source warning"}, Sources: map[string]string{}}, nil
		},
		func(_ context.Context, snapshot *collector.Snapshot, target string, config policy.Config) (*models.Report, error) {
			return &models.Report{ClusterVersion: snapshot.ClusterVersion, TargetVersion: target, PolicyProfile: string(config.Profile), Warnings: snapshot.Warnings}, nil
		},
		policy.Default(),
	)

	report, err := service.Run(context.Background(), models.ScanRequest{
		TargetVersion: "v1.36.2", Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "manifests"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TargetVersion != "1.36" || report.ClusterVersion != "v1.34.1" || len(collected) != 1 || len(report.Warnings) != 1 {
		t.Fatalf("Run() report/selectors = %#v / %#v", report, collected)
	}
}

func TestManifestOnlyScanPreservesDeprecatedAPIEvidenceEndToEnd(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: storagemigration.k8s.io/v1alpha1
kind: StorageVersionMigration
metadata:
  name: legacy-migration
`
	if err := os.WriteFile(filepath.Join(root, "migration.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceScanner, err := sources.New(sources.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	includeCluster := false
	report, err := New(policy.Default(), sourceScanner.Scan).Run(context.Background(), models.ScanRequest{
		CurrentVersion: "1.34", TargetVersion: "1.35", IncludeCluster: &includeCluster,
		Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UpgradeImpact) != 1 {
		t.Fatalf("UpgradeImpact = %#v", report.UpgradeImpact)
	}
	impact := report.UpgradeImpact[0]
	if impact.Rule != "UPG-1.35-STORAGEMIGRATION-V1ALPHA1" || impact.CurrentValue != "storagemigration.k8s.io/v1alpha1" || impact.FieldPath != "apiVersion" || !strings.Contains(impact.Source, "migration.yaml") {
		t.Fatalf("impact = %#v", impact)
	}
}

func TestManifestOnlyScanRequiresCurrentVersion(t *testing.T) {
	includeCluster := false
	service := NewWithDependencies(
		func(context.Context, []models.APIResourceSelector) (*collector.Snapshot, error) {
			t.Fatal("cluster collector should not be called")
			return nil, nil
		},
		func(context.Context, []models.SourceSpec) (*collector.Snapshot, error) {
			return &collector.Snapshot{Sources: map[string]string{}}, nil
		},
		func(_ context.Context, snapshot *collector.Snapshot, _ string, _ policy.Config) (*models.Report, error) {
			return &models.Report{ClusterVersion: snapshot.ClusterVersion}, nil
		},
		policy.Default(),
	)

	_, err := service.Run(context.Background(), models.ScanRequest{
		TargetVersion: "1.36", IncludeCluster: &includeCluster, Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "manifests"}},
	})
	if err == nil {
		t.Fatal("Run() returned no error")
	}
}
