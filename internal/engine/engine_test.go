package engine

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

func TestAnalyzeIncludesUpgradeImpactsInSummaryAndScore(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.35.3",
		Warnings:       []string{"partial evidence"},
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "public"},
			Spec:       corev1.ServiceSpec{ExternalIPs: []string{"203.0.113.20"}},
		}},
	}
	report, err := Analyze(context.Background(), snapshot, "1.36")
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if report.Summary.Medium != 1 || report.Score != 95 || report.ScoreBreakdown.Penalty != 5 {
		t.Fatalf("report summary/score = %#v / %d / %#v", report.Summary, report.Score, report.ScoreBreakdown)
	}
	if len(report.Warnings) != 1 || report.GeneratedAt.IsZero() {
		t.Fatalf("report metadata = %#v", report)
	}
}

func TestCalculateScoreCapsPenalties(t *testing.T) {
	findings := make([]models.Finding, 100)
	for i := range findings {
		findings[i].Severity = models.Low
	}
	score, breakdown := CalculateScore(findings, nil)
	if score != 95 || breakdown.PenaltyApplied.Low != 5 {
		t.Fatalf("CalculateScore() = %d, %#v", score, breakdown)
	}
}

func TestAnalyzeRejectsNilSnapshot(t *testing.T) {
	if _, err := Analyze(context.Background(), nil, "1.36"); err == nil {
		t.Fatal("Analyze(nil) returned no error")
	}
}
