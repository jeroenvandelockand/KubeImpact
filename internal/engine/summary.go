package engine

import "kubeimpact/internal/models"

func BuildSummary(findings []models.Finding, upgradeImpacts []models.UpgradeImpact) models.Summary {
	var summary models.Summary
	for _, finding := range findings {
		incrementSeverity(&summary, finding.Severity)
	}
	for _, impact := range upgradeImpacts {
		incrementSeverity(&summary, impact.Severity)
	}
	return summary
}

func incrementSeverity(summary *models.Summary, severity models.Severity) {
	switch severity {
	case models.Critical:
		summary.Critical++
	case models.High:
		summary.High++
	case models.Medium:
		summary.Medium++
	case models.Low:
		summary.Low++
	case models.Info:
		summary.Info++
	}
}
