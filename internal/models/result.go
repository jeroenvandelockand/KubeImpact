package models

type AnalysisResult struct {
	Findings      []Finding       `json:"findings"`
	UpgradeImpact []UpgradeImpact `json:"upgradeImpact"`
}
