package scan

import (
	"sort"

	"kubeimpact/internal/models"
)

func ApplyComparison(current *models.Report, previous *models.ScanRecord) {
	current.Comparison = models.ReportComparison{ResolvedItems: []models.ResolvedSignal{}}
	previousSignals := map[string]models.ResolvedSignal{}
	if previous != nil && previous.Report != nil {
		current.Comparison.PreviousScanID = previous.ID
		for _, signal := range reportSignals(previous.Report) {
			previousSignals[signal.Fingerprint] = signal
		}
	}

	currentFingerprints := make(map[string]struct{}, len(current.Findings)+len(current.UpgradeImpact)+len(current.Suppressions))
	for _, suppression := range current.Suppressions {
		currentFingerprints[suppression.Fingerprint] = struct{}{}
	}
	for i := range current.Findings {
		fingerprint := current.Findings[i].Fingerprint
		currentFingerprints[fingerprint] = struct{}{}
		if _, exists := previousSignals[fingerprint]; exists {
			current.Findings[i].Change = models.ChangeUnchanged
			current.Comparison.Unchanged++
		} else {
			current.Findings[i].Change = models.ChangeNew
			current.Comparison.New++
		}
	}
	for i := range current.UpgradeImpact {
		fingerprint := current.UpgradeImpact[i].Fingerprint
		currentFingerprints[fingerprint] = struct{}{}
		if _, exists := previousSignals[fingerprint]; exists {
			current.UpgradeImpact[i].Change = models.ChangeUnchanged
			current.Comparison.Unchanged++
		} else {
			current.UpgradeImpact[i].Change = models.ChangeNew
			current.Comparison.New++
		}
	}

	for fingerprint, signal := range previousSignals {
		if _, exists := currentFingerprints[fingerprint]; exists {
			continue
		}
		signal.Change = models.ChangeResolved
		current.Comparison.ResolvedItems = append(current.Comparison.ResolvedItems, signal)
	}
	sort.Slice(current.Comparison.ResolvedItems, func(i, j int) bool {
		left, right := current.Comparison.ResolvedItems[i], current.Comparison.ResolvedItems[j]
		return left.Rule+left.Source+left.Namespace+left.Kind+left.Name+left.Container < right.Rule+right.Source+right.Namespace+right.Kind+right.Name+right.Container
	})
	current.Comparison.Resolved = len(current.Comparison.ResolvedItems)
}

func reportSignals(report *models.Report) []models.ResolvedSignal {
	signals := make([]models.ResolvedSignal, 0, len(report.Findings)+len(report.UpgradeImpact))
	for _, finding := range report.Findings {
		signals = append(signals, models.ResolvedSignal{
			Fingerprint: finding.Fingerprint, Rule: finding.ID, Type: "finding", Severity: finding.Severity,
			Namespace: finding.Namespace, Kind: finding.Kind, Name: finding.Name, Container: finding.Container, Source: finding.Source, Message: finding.Message,
		})
	}
	for _, impact := range report.UpgradeImpact {
		signals = append(signals, models.ResolvedSignal{
			Fingerprint: impact.Fingerprint, Rule: impact.Rule, Type: "upgradeImpact", Severity: impact.Severity,
			Namespace: impact.Namespace, Kind: impact.Kind, Name: impact.Name, Container: impact.Container, Source: impact.Source, Message: impact.Message,
		})
	}
	return signals
}
