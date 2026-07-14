package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"kubeimpact/internal/analyzers"
	"kubeimpact/internal/collector"
	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

func Analyze(ctx context.Context, snapshot *collector.Snapshot, targetVersion string, configs ...policy.Config) (*models.Report, error) {
	if snapshot == nil {
		return nil, errors.New("analyze a nil cluster snapshot")
	}
	if !knowledge.IsSupportedVersion(targetVersion) {
		return nil, fmt.Errorf("unsupported Kubernetes target version %q", knowledge.NormalizeVersion(targetVersion))
	}

	allFindings := make([]models.Finding, 0)
	allUpgradeImpacts := make([]models.UpgradeImpact, 0)
	allSuppressions := make([]models.Suppression, 0)
	selectedPolicy := policy.Default()
	if len(configs) > 0 {
		selectedPolicy = configs[0]
	}
	for _, analyzer := range analyzers.Default(knowledge.NormalizeVersion(targetVersion), selectedPolicy) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := analyzer.Analyze(ctx, snapshot)
		if err != nil {
			return nil, fmt.Errorf("%s analyzer failed: %w", analyzer.Name(), err)
		}
		if result == nil {
			return nil, fmt.Errorf("%s analyzer returned no result", analyzer.Name())
		}
		allFindings = append(allFindings, result.Findings...)
		allUpgradeImpacts = append(allUpgradeImpacts, result.UpgradeImpact...)
		allSuppressions = append(allSuppressions, result.Suppressions...)
	}
	allFindings = uniqueFindings(allFindings)
	allUpgradeImpacts = uniqueUpgradeImpacts(allUpgradeImpacts)
	allSuppressions = uniqueSuppressions(allSuppressions)

	score, scoreBreakdown := CalculateScore(allFindings, allUpgradeImpacts)
	return &models.Report{
		ClusterVersion:    snapshot.ClusterVersion,
		TargetVersion:     knowledge.NormalizeVersion(targetVersion),
		GeneratedAt:       time.Now().UTC(),
		PolicyProfile:     string(selectedPolicy.Profile),
		PolicyFingerprint: selectedPolicy.Fingerprint(),
		Score:             score,
		ScoreBreakdown:    scoreBreakdown,
		Summary:           BuildSummary(allFindings, allUpgradeImpacts),
		Warnings:          append([]string{}, snapshot.Warnings...),
		Sources:           append([]models.SourceResult{}, snapshot.SourceResults...),
		Suppressions:      allSuppressions,
		Findings:          allFindings,
		UpgradeImpact:     allUpgradeImpacts,
	}, nil
}

func uniqueFindings(values []models.Finding) []models.Finding {
	seen := make(map[string]struct{}, len(values))
	result := make([]models.Finding, 0, len(values))
	for _, value := range values {
		if value.Fingerprint != "" {
			if _, exists := seen[value.Fingerprint]; exists {
				continue
			}
			seen[value.Fingerprint] = struct{}{}
		}
		result = append(result, value)
	}
	return result
}

func uniqueUpgradeImpacts(values []models.UpgradeImpact) []models.UpgradeImpact {
	seen := make(map[string]struct{}, len(values))
	result := make([]models.UpgradeImpact, 0, len(values))
	for _, value := range values {
		if value.Fingerprint != "" {
			if _, exists := seen[value.Fingerprint]; exists {
				continue
			}
			seen[value.Fingerprint] = struct{}{}
		}
		result = append(result, value)
	}
	return result
}

func uniqueSuppressions(values []models.Suppression) []models.Suppression {
	seen := make(map[string]struct{}, len(values))
	result := make([]models.Suppression, 0, len(values))
	for _, value := range values {
		if value.Fingerprint != "" {
			if _, exists := seen[value.Fingerprint]; exists {
				continue
			}
			seen[value.Fingerprint] = struct{}{}
		}
		result = append(result, value)
	}
	return result
}
