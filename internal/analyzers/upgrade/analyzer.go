package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
)

type Analyzer struct {
	targetVersion string
}

func New(targetVersion string) *Analyzer {
	return &Analyzer{targetVersion: targetVersion}
}

func (a *Analyzer) Name() string {
	return "upgrade"
}

func (a *Analyzer) Analyze(ctx context.Context, snapshot *collector.Snapshot) (*models.AnalysisResult, error) {
	if snapshot == nil {
		return nil, errors.New("upgrade analyzer received a nil snapshot")
	}

	ruleSets, err := knowledge.LoadForUpgrade(snapshot.ClusterVersion, a.targetVersion)
	if err != nil {
		return nil, err
	}

	impacts := make([]models.UpgradeImpact, 0)
	for _, rules := range ruleSets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		for _, resource := range snapshot.Resources {
			for _, rule := range rules.RemovedAPIs {
				if observedVersion(resource, rule.GroupVersion) && resource.Kind == rule.Kind {
					impacts = append(impacts, apiImpact(resource, rule, models.Critical, "removed API"))
				}
			}
			for _, rule := range rules.DeprecatedAPIs {
				if observedVersion(resource, rule.GroupVersion) && resource.Kind == rule.Kind {
					impacts = append(impacts, apiImpact(resource, rule, models.Medium, "deprecated API"))
				}
			}
		}

		for _, check := range rules.ResourceChecks {
			checkImpacts, checkErr := a.evaluateResourceCheck(check, snapshot)
			if checkErr != nil {
				return nil, checkErr
			}
			impacts = append(impacts, checkImpacts...)
		}
	}

	return &models.AnalysisResult{UpgradeImpact: impacts}, nil
}

func (a *Analyzer) evaluateResourceCheck(check knowledge.ResourceCheck, snapshot *collector.Snapshot) ([]models.UpgradeImpact, error) {
	switch check.Name {
	case "serviceExternalIPs":
		impacts := make([]models.UpgradeImpact, 0)
		for i := range snapshot.Services {
			service := &snapshot.Services[i]
			if len(service.Spec.ExternalIPs) == 0 {
				continue
			}
			fieldPath := "spec.externalIPs"
			impacts = append(impacts, models.UpgradeImpact{
				Rule:             check.ID,
				Fingerprint:      models.NewFingerprint(a.Name(), check.ID, service.Namespace, "Service", service.Name, fieldPath),
				Severity:         models.Severity(check.Severity),
				Category:         "api",
				Namespace:        service.Namespace,
				Kind:             "Service",
				Name:             service.Name,
				FieldPath:        fieldPath,
				CurrentValue:     strings.Join(service.Spec.ExternalIPs, ", "),
				ExpectedValue:    "no externalIPs",
				Message:          check.Message,
				Recommendation:   check.Recommendation,
				DocumentationURL: check.DocumentationURL,
			})
		}
		return impacts, nil
	default:
		return nil, fmt.Errorf("upgrade rule %s uses unknown resource check %q", check.ID, check.Name)
	}
}

func observedVersion(resource models.KubernetesResource, wanted string) bool {
	for _, version := range resource.ObservedAPIVersions {
		if version == wanted {
			return true
		}
	}
	return false
}

func apiImpact(resource models.KubernetesResource, rule knowledge.APIRule, severity models.Severity, expected string) models.UpgradeImpact {
	return models.UpgradeImpact{
		Rule:             rule.ID,
		Fingerprint:      models.NewFingerprint("upgrade", rule.ID, resource.Namespace, resource.Kind, resource.Name, rule.GroupVersion),
		Severity:         severity,
		Category:         "api",
		Namespace:        resource.Namespace,
		Kind:             resource.Kind,
		Name:             resource.Name,
		FieldPath:        "metadata.managedFields[].apiVersion",
		CurrentValue:     rule.GroupVersion,
		ExpectedValue:    expected,
		Message:          rule.Message,
		Recommendation:   rule.Recommendation,
		DocumentationURL: rule.DocumentationURL,
	}
}
