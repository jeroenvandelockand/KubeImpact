package upgrade

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

func TestAnalyzeLoadsTheCompleteUpgradePath(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.34.9",
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "edge", Name: "legacy"},
			Spec:       corev1.ServiceSpec{ExternalIPs: []string{"203.0.113.10"}},
		}},
		Resources: []models.KubernetesResource{{
			Kind:                "StorageVersionMigration",
			Name:                "migration",
			ObservedAPIVersions: []string{"storagemigration.k8s.io/v1alpha1"},
		}},
	}

	result, err := New("1.36").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	wanted := map[string]bool{
		"UPG-1.35-STORAGEMIGRATION-V1ALPHA1": false,
		"UPG-1.36-SERVICE-EXTERNALIPS":       false,
	}
	for _, impact := range result.UpgradeImpact {
		wanted[impact.Rule] = true
		if impact.Fingerprint == "" || impact.DocumentationURL == "" {
			t.Errorf("impact %s is missing evidence metadata", impact.Rule)
		}
	}
	for rule, found := range wanted {
		if !found {
			t.Errorf("expected impact %s", rule)
		}
	}
}

func TestAnalyzeDoesNotInferAPIUsageWithoutManagedFieldsEvidence(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.34.0",
		Resources: []models.KubernetesResource{{
			Kind: "StorageVersionMigration",
			Name: "migration",
		}},
	}
	result, err := New("1.35").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(result.UpgradeImpact) != 0 {
		t.Fatalf("UpgradeImpact = %#v, want none", result.UpgradeImpact)
	}
}

func TestAnalyzeRejectsNonUpgradeTarget(t *testing.T) {
	_, err := New("1.35").Analyze(context.Background(), &collector.Snapshot{ClusterVersion: "v1.35.2"})
	if err == nil {
		t.Fatal("Analyze() returned no error")
	}
}
