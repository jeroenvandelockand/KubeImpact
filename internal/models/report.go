package models

import "time"

type Summary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

type Report struct {
	ScanID            string    `json:"scanId"`
	ClusterVersion    string    `json:"clusterVersion"`
	TargetVersion     string    `json:"targetVersion"`
	GeneratedAt       time.Time `json:"generatedAt"`
	PolicyProfile     string    `json:"policyProfile"`
	PolicyFingerprint string    `json:"policyFingerprint"`

	Score          int            `json:"score"`
	ScoreBreakdown ScoreBreakdown `json:"scoreBreakdown"`

	Summary      Summary          `json:"summary"`
	Warnings     []string         `json:"warnings"`
	Sources      []SourceResult   `json:"sources"`
	Suppressions []Suppression    `json:"suppressions"`
	Comparison   ReportComparison `json:"comparison"`

	Findings []Finding `json:"findings"`

	UpgradeImpact []UpgradeImpact `json:"upgradeImpact"`
}

type ScoreBreakdown struct {
	BaseScore      int     `json:"baseScore"`
	Penalty        int     `json:"penalty"`
	PenaltyCaps    Summary `json:"penaltyCaps"`
	PenaltyApplied Summary `json:"penaltyApplied"`
}
