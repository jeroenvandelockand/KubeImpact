package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
		if rules.Version == "1.37" {
			appendWarning(snapshot, "SELinuxMount conflict coverage depends on the optional Kubernetes 1.36 selinux-warning-controller emitting Events; enable that controller and review its metrics before upgrading clusters that enforce SELinux.")
		}

		for _, resource := range snapshot.Resources {
			for _, rule := range rules.RemovedAPIs {
				if ruleApplies(resource, rule) {
					impacts = append(impacts, apiImpact(resource, rule, models.Critical, "removed API"))
				}
			}
			for _, rule := range rules.DeprecatedAPIs {
				if ruleApplies(resource, rule) {
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

	for _, request := range snapshot.DeprecatedAPIRequests {
		if !releaseAtOrBefore(request.RemovedRelease, a.targetVersion) {
			continue
		}
		name := request.Resource
		if request.Subresource != "" {
			name += "/" + request.Subresource
		}
		const ruleID = "UPG-API-REQUEST-001"
		impacts = append(impacts, models.UpgradeImpact{
			Rule: ruleID, Fingerprint: models.NewFingerprint(a.Name(), ruleID, request.GroupVersion, name, request.RemovedRelease),
			Severity: models.Critical, Category: "api", Kind: "APIResource", Name: name, Source: "apiserver-metrics",
			FieldPath: "apiserver_requested_deprecated_apis", CurrentValue: request.GroupVersion + " requested", ExpectedValue: "no requests",
			Message:          fmt.Sprintf("The API server observed requests to %s %s, which is removed in Kubernetes %s.", request.GroupVersion, name, request.RemovedRelease),
			Recommendation:   "Update every client and generated manifest using this API, then confirm the deprecated-API metric is no longer emitted before upgrading.",
			DocumentationURL: "https://kubernetes.io/docs/reference/using-api/deprecation-guide/",
		})
	}

	return &models.AnalysisResult{UpgradeImpact: impacts}, nil
}

func ruleApplies(resource models.KubernetesResource, rule knowledge.APIRule) bool {
	if rule.SourceOnly && (resource.Source == "" || resource.Source == "cluster") {
		return false
	}
	if rule.MatchPresence && resource.Source == "cluster" && resource.ListedAPIVersion == rule.GroupVersion {
		return resource.Kind == rule.Kind
	}
	return resource.Kind == rule.Kind && observedVersion(resource, rule.GroupVersion)
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
			source := collector.ObjectSource(service.Annotations)
			impacts = append(impacts, models.UpgradeImpact{
				Rule:             check.ID,
				Fingerprint:      models.NewFingerprint(a.Name(), check.ID, source, service.Namespace, "Service", service.Name, fieldPath),
				Severity:         models.Severity(check.Severity),
				Category:         "api",
				Namespace:        service.Namespace,
				Kind:             "Service",
				Name:             service.Name,
				FieldPath:        fieldPath,
				Source:           source,
				CurrentValue:     strings.Join(service.Spec.ExternalIPs, ", "),
				ExpectedValue:    "no externalIPs",
				Message:          check.Message,
				Recommendation:   check.Recommendation,
				DocumentationURL: check.DocumentationURL,
			})
		}
		return impacts, nil
	case "kubeletEventRecordQPSZero":
		return evaluateKubeletEventRecordQPS(check, snapshot), nil
	case "selinuxVolumeConflict":
		return evaluateSELinuxVolumeConflicts(check, snapshot), nil
	case "kubeProxyIPVS":
		return evaluateKubeProxyIPVS(check, snapshot), nil
	case "kubeProxyModeUnset":
		return evaluateKubeProxyModeUnset(check, snapshot), nil
	case "kubeDNS":
		return evaluateKubeDNS(check, snapshot), nil
	case "nodeLogsRBAC":
		return evaluateNodeLogsRBAC(check, snapshot), nil
	case "staticPodAPIReference":
		return evaluateStaticPodAPIReferences(check, snapshot), nil
	case "kubeletCgroupV1Override":
		return evaluateKubeletCgroupV1Override(check, snapshot), nil
	case "cloudNodeMonitorPeriod":
		return evaluateCloudNodeMonitorPeriod(check, snapshot), nil
	case "lockedFeatureGate":
		return evaluateLockedFeatureGates(check, snapshot), nil
	case "removedFeatureGate":
		return evaluateRemovedFeatureGates(check, snapshot), nil
	case "removedComponentFlag":
		return evaluateRemovedComponentFlags(check, snapshot), nil
	default:
		return nil, fmt.Errorf("upgrade rule %s uses unknown resource check %q", check.ID, check.Name)
	}
}

func appendWarning(snapshot *collector.Snapshot, warning string) {
	for _, existing := range snapshot.Warnings {
		if existing == warning {
			return
		}
	}
	snapshot.Warnings = append(snapshot.Warnings, warning)
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
	fieldPath := "apiVersion"
	if resource.Source == "" || resource.Source == "cluster" {
		if rule.MatchPresence && resource.ListedAPIVersion == rule.GroupVersion {
			fieldPath = "listedApiVersion"
		} else {
			fieldPath = "metadata.managedFields[].apiVersion"
		}
	}
	return models.UpgradeImpact{
		Rule:             rule.ID,
		Fingerprint:      models.NewFingerprint("upgrade", rule.ID, resource.Source, resource.Namespace, resource.Kind, resource.Name, rule.GroupVersion),
		Severity:         severity,
		Category:         "api",
		Namespace:        resource.Namespace,
		Kind:             resource.Kind,
		Name:             resource.Name,
		FieldPath:        fieldPath,
		Source:           resource.Source,
		CurrentValue:     rule.GroupVersion,
		ExpectedValue:    expected,
		Message:          rule.Message,
		Recommendation:   rule.Recommendation,
		DocumentationURL: rule.DocumentationURL,
	}
}

func releaseAtOrBefore(release, target string) bool {
	parse := func(value string) (int, int, bool) {
		parts := strings.Split(knowledge.NormalizeVersion(value), ".")
		if len(parts) != 2 {
			return 0, 0, false
		}
		major, majorErr := strconv.Atoi(parts[0])
		minor, minorErr := strconv.Atoi(parts[1])
		return major, minor, majorErr == nil && minorErr == nil
	}
	releaseMajor, releaseMinor, releaseOK := parse(release)
	targetMajor, targetMinor, targetOK := parse(target)
	return releaseOK && targetOK && (releaseMajor < targetMajor || releaseMajor == targetMajor && releaseMinor <= targetMinor)
}
