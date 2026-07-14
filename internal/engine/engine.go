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
)

func Analyze(ctx context.Context, snapshot *collector.Snapshot, targetVersion string) (*models.Report, error) {
	if snapshot == nil {
		return nil, errors.New("analyze a nil cluster snapshot")
	}
	if !knowledge.IsSupportedVersion(targetVersion) {
		return nil, fmt.Errorf("unsupported Kubernetes target version %q", knowledge.NormalizeVersion(targetVersion))
	}

	allFindings := make([]models.Finding, 0)
	allUpgradeImpacts := make([]models.UpgradeImpact, 0)
	for _, analyzer := range analyzers.Default(knowledge.NormalizeVersion(targetVersion)) {
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
	}

	score, scoreBreakdown := CalculateScore(allFindings, allUpgradeImpacts)
	return &models.Report{
		ClusterVersion: snapshot.ClusterVersion,
		TargetVersion:  knowledge.NormalizeVersion(targetVersion),
		GeneratedAt:    time.Now().UTC(),
		Score:          score,
		ScoreBreakdown: scoreBreakdown,
		Summary:        BuildSummary(allFindings, allUpgradeImpacts),
		Warnings:       append([]string{}, snapshot.Warnings...),
		Findings:       allFindings,
		UpgradeImpact:  allUpgradeImpacts,
	}, nil
}
