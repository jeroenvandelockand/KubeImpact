package workload

import (
	"context"
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

const (
	ignoreAnnotation       = "kubeimpact.io/ignore"
	ignoreReasonAnnotation = "kubeimpact.io/ignore-reason"
)

type Analyzer struct {
	policy policy.Config
}

func New(config ...policy.Config) *Analyzer {
	selected := policy.Default()
	if len(config) > 0 {
		selected = config[0]
	}
	return &Analyzer{policy: selected}
}

func (a *Analyzer) Name() string { return "workload" }

func (a *Analyzer) Analyze(ctx context.Context, snapshot *collector.Snapshot) (*models.AnalysisResult, error) {
	if snapshot == nil {
		return nil, errors.New("workload analyzer received a nil snapshot")
	}

	result := &models.AnalysisResult{
		Findings:     []models.Finding{},
		Suppressions: []models.Suppression{},
	}
	analyze := func(target workloadTarget) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.policy.Excludes(target.namespace, target.labels, snapshot.NamespaceLabels(target.namespace, target.sourceScope)) {
			return nil
		}

		findings := a.evaluate(target)
		ignored, reason := ignoredRules(target.annotations)
		invalidSuppressionReported := false
		for _, finding := range findings {
			finding.Severity = a.policy.Severity(finding.ID, finding.Severity)
			if ignored[finding.ID] || ignored["*"] {
				if reason != "" {
					result.Suppressions = append(result.Suppressions, models.Suppression{
						RuleID:      finding.ID,
						Namespace:   finding.Namespace,
						Kind:        finding.Kind,
						Name:        finding.Name,
						Container:   finding.Container,
						Source:      finding.Source,
						Reason:      reason,
						Fingerprint: finding.Fingerprint,
					})
					continue
				}
				if !invalidSuppressionReported {
					result.Findings = append(result.Findings, a.invalidSuppressionFinding(target))
					invalidSuppressionReported = true
				}
			}
			result.Findings = append(result.Findings, finding)
		}
		return nil
	}

	for i := range snapshot.Deployments {
		workload := &snapshot.Deployments[i]
		if err := analyze(newTarget("Deployment", workload.Namespace, workload.Name, workload.Labels, workload.Annotations, workload.Spec.Template.ObjectMeta, &workload.Spec.Template.Spec, snapshot)); err != nil {
			return nil, err
		}
	}
	for i := range snapshot.StatefulSets {
		workload := &snapshot.StatefulSets[i]
		if err := analyze(newTarget("StatefulSet", workload.Namespace, workload.Name, workload.Labels, workload.Annotations, workload.Spec.Template.ObjectMeta, &workload.Spec.Template.Spec, snapshot)); err != nil {
			return nil, err
		}
	}
	for i := range snapshot.DaemonSets {
		workload := &snapshot.DaemonSets[i]
		if err := analyze(newTarget("DaemonSet", workload.Namespace, workload.Name, workload.Labels, workload.Annotations, workload.Spec.Template.ObjectMeta, &workload.Spec.Template.Spec, snapshot)); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func newTarget(kind, namespace, name string, labels, annotations map[string]string, templateMeta metav1.ObjectMeta, podSpec *corev1.PodSpec, snapshot *collector.Snapshot) workloadTarget {
	mergedAnnotations := make(map[string]string, len(annotations)+len(templateMeta.Annotations))
	for key, value := range annotations {
		mergedAnnotations[key] = value
	}
	for key, value := range templateMeta.Annotations {
		mergedAnnotations[key] = value
	}
	return workloadTarget{
		kind:        kind,
		namespace:   namespace,
		name:        name,
		labels:      labels,
		annotations: mergedAnnotations,
		podSpec:     podSpec,
		source:      collector.ObjectSource(mergedAnnotations),
		sourceScope: collector.ObjectSourceScope(mergedAnnotations),
	}
}

func (a *Analyzer) evaluate(target workloadTarget) []models.Finding {
	findings := make([]models.Finding, 0)
	for _, rule := range podRules() {
		if !a.policy.RuleEnabled(rule.profile) {
			continue
		}
		for _, violation := range rule.evaluate(target) {
			findings = append(findings, a.finding(target, violation))
		}
	}

	for _, container := range containers(target.podSpec) {
		for _, rule := range containerRules() {
			if !a.policy.RuleEnabled(rule.profile) {
				continue
			}
			for _, violation := range rule.evaluate(target, container) {
				findings = append(findings, a.finding(target, violation))
			}
		}
	}
	return findings
}

func (a *Analyzer) finding(target workloadTarget, violation violation) models.Finding {
	fieldPath := violation.fieldPath
	return models.Finding{
		ID:               violation.id,
		Fingerprint:      models.NewFingerprint(a.Name(), violation.id, target.source, target.namespace, target.kind, target.name, violation.container, fieldPath),
		Analyzer:         a.Name(),
		Severity:         violation.severity,
		Category:         violation.category,
		Namespace:        target.namespace,
		Kind:             target.kind,
		Name:             target.name,
		Container:        violation.container,
		FieldPath:        fieldPath,
		Source:           target.source,
		CurrentValue:     violation.currentValue,
		ExpectedValue:    violation.expectedValue,
		Message:          violation.message,
		Recommendation:   violation.recommendation,
		DocumentationURL: violation.documentationURL,
	}
}

func (a *Analyzer) invalidSuppressionFinding(target workloadTarget) models.Finding {
	const id = "WLK-SUPPRESSION-INVALID-001"
	return models.Finding{
		ID:               id,
		Fingerprint:      models.NewFingerprint(a.Name(), id, target.source, target.namespace, target.kind, target.name),
		Analyzer:         a.Name(),
		Severity:         a.policy.Severity(id, models.Info),
		Category:         "governance",
		Namespace:        target.namespace,
		Kind:             target.kind,
		Name:             target.name,
		FieldPath:        "metadata.annotations",
		Source:           target.source,
		CurrentValue:     ignoreAnnotation + " without " + ignoreReasonAnnotation,
		ExpectedValue:    "a documented suppression reason",
		Message:          "KubeImpact suppression was ignored because it has no reason.",
		Recommendation:   "Add kubeimpact.io/ignore-reason with a reviewable justification or remove the suppression annotation.",
		DocumentationURL: podSecurityStandardsURL,
	}
}

func ignoredRules(annotations map[string]string) (map[string]bool, string) {
	ignored := make(map[string]bool)
	for _, id := range strings.Split(annotations[ignoreAnnotation], ",") {
		if id = strings.TrimSpace(id); id != "" {
			ignored[id] = true
		}
	}
	return ignored, strings.TrimSpace(annotations[ignoreReasonAnnotation])
}
