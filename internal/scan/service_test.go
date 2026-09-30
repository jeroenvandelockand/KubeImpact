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
		func(_ context.Context, _ []models.SourceSpec, _ string) (*collector.Snapshot, error) {
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
	report, err := New(policy.Default(), sourceScanner.ScanForVersion).Run(context.Background(), models.ScanRequest{
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

func TestManifestOnlyScanDetectsKubernetes137RemovedAPIs(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: scheduling.k8s.io/v1alpha2
kind: Workload
metadata: {name: checkout, namespace: shop}
---
apiVersion: scheduling.k8s.io/v1alpha2
kind: PodGroup
metadata: {name: checkout, namespace: shop}
---
apiVersion: kubeadm.k8s.io/v1beta3
kind: InitConfiguration
---
apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
---
apiVersion: kubeadm.k8s.io/v1beta3
kind: JoinConfiguration
`
	if err := os.WriteFile(filepath.Join(root, "removed.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceScanner, err := sources.New(sources.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	includeCluster := false
	report, err := New(policy.Default(), sourceScanner.ScanForVersion).Run(context.Background(), models.ScanRequest{
		CurrentVersion: "1.36", TargetVersion: "1.37", IncludeCluster: &includeCluster,
		Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"UPG-1.37-WORKLOAD-V1ALPHA2":       false,
		"UPG-1.37-PODGROUP-V1ALPHA2":       false,
		"UPG-1.37-KUBEADM-INIT-V1BETA3":    false,
		"UPG-1.37-KUBEADM-CLUSTER-V1BETA3": false,
		"UPG-1.37-KUBEADM-JOIN-V1BETA3":    false,
	}
	for _, impact := range report.UpgradeImpact {
		if _, exists := wanted[impact.Rule]; exists {
			wanted[impact.Rule] = true
		}
	}
	for rule, found := range wanted {
		if !found {
			t.Errorf("missing impact %s in %#v", rule, report.UpgradeImpact)
		}
	}
}

func TestManifestOnlyScanDetectsKubernetes137ConfigurationChanges(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
eventRecordQPS: 0
failCgroupV1: false
---
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
mode: ipvs
---
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: node-log-reader}
rules:
  - apiGroups: [""]
    resources: ["nodes/logs"]
    verbs: ["get"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-dns
  namespace: kube-system
  labels: {k8s-app: kube-dns}
spec:
  selector:
    matchLabels: {k8s-app: kube-dns}
  template:
    metadata:
      labels: {k8s-app: kube-dns}
    spec:
      containers: [{name: dns, image: registry.k8s.io/k8s-dns-kube-dns:1.22.28}]
---
apiVersion: v1
kind: Event
metadata:
  name: selinux-conflict
  namespace: payments
reason: SELinuxLabelConflict
message: a conflicting pod uses the same volume with a different SELinux label
source:
  component: selinux_warning
involvedObject:
  apiVersion: v1
  kind: Pod
  namespace: payments
  name: checkout-0
---
apiVersion: v1
kind: Pod
metadata:
  name: static-api-client
  namespace: kube-system
  annotations:
    kubernetes.io/config.source: file
spec:
  containers:
    - name: client
      image: example/client:1
      volumeMounts: [{name: config, mountPath: /etc/config}]
  volumes:
    - name: config
      configMap: {name: static-config}
`
	if err := os.WriteFile(filepath.Join(root, "configuration.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceScanner, err := sources.New(sources.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	includeCluster := false
	report, err := New(policy.Default(), sourceScanner.ScanForVersion).Run(context.Background(), models.ScanRequest{
		CurrentVersion: "1.36", TargetVersion: "1.37", IncludeCluster: &includeCluster,
		Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"UPG-1.37-KUBELET-EVENTRECORDQPS-ZERO": false,
		"UPG-1.37-SELINUX-VOLUME-CONFLICT":     false,
		"UPG-1.37-KUBEPROXY-IPVS":              false,
		"UPG-1.37-KUBEPROXY-MODE-UNSET":        false,
		"UPG-1.37-KUBEDNS":                     false,
		"UPG-1.37-NODE-LOGS-RBAC":              false,
		"UPG-1.37-STATIC-POD-API-REFERENCE":    false,
		"UPG-1.37-CGROUP-V1-OVERRIDE":          false,
	}
	for _, impact := range report.UpgradeImpact {
		if _, exists := wanted[impact.Rule]; exists {
			wanted[impact.Rule] = true
			if impact.Source == "" || impact.FieldPath == "" || impact.Recommendation == "" {
				t.Errorf("impact lacks actionable evidence: %#v", impact)
			}
			if impact.Rule == "UPG-1.37-CGROUP-V1-OVERRIDE" && impact.Severity != models.Info {
				t.Errorf("cgroup v1 advisory severity = %q, want info", impact.Severity)
			}
		}
	}
	for rule, found := range wanted {
		if !found {
			t.Errorf("missing impact %s in %#v", rule, report.UpgradeImpact)
		}
	}
	if len(report.Warnings) == 0 || !strings.Contains(strings.Join(report.Warnings, "\n"), "selinux-warning-controller") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
}

func TestManifestOnlyScanDetectsKubernetes137DeprecatedAPIs(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: certificates.k8s.io/v1beta1
kind: ClusterTrustBundle
metadata: {name: legacy-trust}
---
apiVersion: certificates.k8s.io/v1beta1
kind: PodCertificateRequest
metadata: {name: legacy-pod-certificate}
---
apiVersion: storagemigration.k8s.io/v1beta1
kind: StorageVersionMigration
metadata: {name: legacy-migration}
`
	if err := os.WriteFile(filepath.Join(root, "deprecated.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceScanner, err := sources.New(sources.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	includeCluster := false
	report, err := New(policy.Default(), sourceScanner.ScanForVersion).Run(context.Background(), models.ScanRequest{
		CurrentVersion: "1.36", TargetVersion: "1.37", IncludeCluster: &includeCluster,
		Sources: []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"UPG-1.37-CLUSTERTRUSTBUNDLE-V1BETA1":    false,
		"UPG-1.37-PODCERTIFICATEREQUEST-V1BETA1": false,
		"UPG-1.37-STORAGEMIGRATION-V1BETA1":      false,
	}
	for _, impact := range report.UpgradeImpact {
		if _, exists := wanted[impact.Rule]; exists {
			wanted[impact.Rule] = true
			if impact.Severity != models.Medium || impact.ExpectedValue != "deprecated API" {
				t.Errorf("deprecated API impact = %#v", impact)
			}
		}
	}
	for rule, found := range wanted {
		if !found {
			t.Errorf("missing impact %s in %#v", rule, report.UpgradeImpact)
		}
	}
}

func TestManifestOnlyScanRequiresCurrentVersion(t *testing.T) {
	includeCluster := false
	service := NewWithDependencies(
		func(context.Context, []models.APIResourceSelector) (*collector.Snapshot, error) {
			t.Fatal("cluster collector should not be called")
			return nil, nil
		},
		func(context.Context, []models.SourceSpec, string) (*collector.Snapshot, error) {
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
