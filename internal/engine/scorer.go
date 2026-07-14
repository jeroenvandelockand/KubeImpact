package engine

import "kubeimpact/internal/models"

var weights = map[models.Severity]int{
	models.Critical: 25,
	models.High:     10,
	models.Medium:   5,
	models.Low:      2,
	models.Info:     0,
}

var penaltyCaps = models.Summary{
	Critical: 50,
	High:     30,
	Medium:   15,
	Low:      5,
	Info:     0,
}

func CalculateScore(findings []models.Finding, upgradeImpacts []models.UpgradeImpact) (int, models.ScoreBreakdown) {
	counts := BuildSummary(findings, upgradeImpacts)
	applied := models.Summary{
		Critical: cappedPenalty(counts.Critical, models.Critical, penaltyCaps.Critical),
		High:     cappedPenalty(counts.High, models.High, penaltyCaps.High),
		Medium:   cappedPenalty(counts.Medium, models.Medium, penaltyCaps.Medium),
		Low:      cappedPenalty(counts.Low, models.Low, penaltyCaps.Low),
		Info:     0,
	}
	penalty := applied.Critical + applied.High + applied.Medium + applied.Low
	score := max(0, 100-penalty)

	return score, models.ScoreBreakdown{
		BaseScore:      100,
		Penalty:        penalty,
		PenaltyCaps:    penaltyCaps,
		PenaltyApplied: applied,
	}
}

func cappedPenalty(count int, severity models.Severity, cap int) int {
	return min(count*weights[severity], cap)
}
