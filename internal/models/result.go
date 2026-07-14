package models

type AnalysisResult struct {
	Findings      []Finding       `json:"findings"`
	UpgradeImpact []UpgradeImpact `json:"upgradeImpact"`
	Suppressions  []Suppression   `json:"suppressions"`
}
