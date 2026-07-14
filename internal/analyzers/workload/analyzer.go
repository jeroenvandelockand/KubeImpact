package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

const (
	podSecurityStandardsURL = "https://kubernetes.io/docs/concepts/security/pod-security-standards/"
	resourceManagementURL   = "https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/"
)

type Analyzer struct{}

func New() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Name() string {
	return "workload"
}

func (a *Analyzer) Analyze(ctx context.Context, snapshot *collector.Snapshot) (*models.AnalysisResult, error) {
	if snapshot == nil {
		return nil, errors.New("workload analyzer received a nil snapshot")
	}

	findings := make([]models.Finding, 0)

	for i := range snapshot.Deployments {
		deployment := &snapshot.Deployments[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		findings = append(findings, a.analyzePodSpec(deployment.Namespace, "Deployment", deployment.Name, &deployment.Spec.Template.Spec)...)
	}
	for i := range snapshot.StatefulSets {
		statefulSet := &snapshot.StatefulSets[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		findings = append(findings, a.analyzePodSpec(statefulSet.Namespace, "StatefulSet", statefulSet.Name, &statefulSet.Spec.Template.Spec)...)
	}
	for i := range snapshot.DaemonSets {
		daemonSet := &snapshot.DaemonSets[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		findings = append(findings, a.analyzePodSpec(daemonSet.Namespace, "DaemonSet", daemonSet.Name, &daemonSet.Spec.Template.Spec)...)
	}

	return &models.AnalysisResult{Findings: findings}, nil
}

type containerView struct {
	name            string
	fieldPath       string
	resources       corev1.ResourceRequirements
	securityContext *corev1.SecurityContext
	checkResources  bool
}

func (a *Analyzer) analyzePodSpec(namespace, kind, name string, podSpec *corev1.PodSpec) []models.Finding {
	findings := make([]models.Finding, 0)
	appendPodFinding := func(id string, severity models.Severity, category, fieldPath, currentValue, message, recommendation, documentationURL string) {
		findings = append(findings, models.Finding{
			ID:               id,
			Fingerprint:      models.NewFingerprint(a.Name(), id, namespace, kind, name, fieldPath),
			Analyzer:         a.Name(),
			Severity:         severity,
			Category:         category,
			Namespace:        namespace,
			Kind:             kind,
			Name:             name,
			FieldPath:        fieldPath,
			CurrentValue:     currentValue,
			ExpectedValue:    "false",
			Message:          message,
			Recommendation:   recommendation,
			DocumentationURL: documentationURL,
		})
	}

	if podSpec.HostNetwork {
		appendPodFinding(
			"WLK-HOSTNET-001", models.Medium, "network", "spec.template.spec.hostNetwork", "true",
			"Pod uses the node network namespace (spec.template.spec.hostNetwork=true).",
			"Remove hostNetwork: true unless node-network access is required; document and constrain any exception.",
			podSecurityStandardsURL,
		)
	}
	if podSpec.HostPID {
		appendPodFinding(
			"WLK-HOSTPID-001", models.High, "security", "spec.template.spec.hostPID", "true",
			"Pod shares the node process ID namespace.",
			"Remove hostPID: true unless node process access is strictly required.",
			podSecurityStandardsURL,
		)
	}
	if podSpec.HostIPC {
		appendPodFinding(
			"WLK-HOSTIPC-001", models.High, "security", "spec.template.spec.hostIPC", "true",
			"Pod shares the node IPC namespace.",
			"Remove hostIPC: true unless node IPC access is strictly required.",
			podSecurityStandardsURL,
		)
	}

	containers := make([]containerView, 0, len(podSpec.Containers)+len(podSpec.InitContainers)+len(podSpec.EphemeralContainers))
	for i := range podSpec.Containers {
		container := &podSpec.Containers[i]
		containers = append(containers, containerView{
			name:            container.Name,
			fieldPath:       fmt.Sprintf("spec.template.spec.containers[%s]", container.Name),
			resources:       container.Resources,
			securityContext: container.SecurityContext,
			checkResources:  true,
		})
	}
	for i := range podSpec.InitContainers {
		container := &podSpec.InitContainers[i]
		containers = append(containers, containerView{
			name:            container.Name,
			fieldPath:       fmt.Sprintf("spec.template.spec.initContainers[%s]", container.Name),
			resources:       container.Resources,
			securityContext: container.SecurityContext,
			checkResources:  true,
		})
	}
	for i := range podSpec.EphemeralContainers {
		container := &podSpec.EphemeralContainers[i]
		containers = append(containers, containerView{
			name:            container.Name,
			fieldPath:       fmt.Sprintf("spec.template.spec.ephemeralContainers[%s]", container.Name),
			resources:       container.Resources,
			securityContext: container.SecurityContext,
		})
	}

	isWindows := podSpec.OS != nil && podSpec.OS.Name == corev1.Windows
	if podSpec.NodeSelector[corev1.LabelOSStable] == string(corev1.Windows) || podSpec.NodeSelector["beta.kubernetes.io/os"] == string(corev1.Windows) {
		isWindows = true
	}
	for _, container := range containers {
		findings = append(findings, a.analyzeContainer(namespace, kind, name, podSpec.SecurityContext, container, isWindows)...)
	}

	return findings
}

func (a *Analyzer) analyzeContainer(namespace, kind, workloadName string, podSecurityContext *corev1.PodSecurityContext, container containerView, isWindows bool) []models.Finding {
	findings := make([]models.Finding, 0)
	appendFinding := func(id string, severity models.Severity, category, fieldSuffix, currentValue, expectedValue, message, recommendation, documentationURL string) {
		fieldPath := container.fieldPath + fieldSuffix
		findings = append(findings, models.Finding{
			ID:               id,
			Fingerprint:      models.NewFingerprint(a.Name(), id, namespace, kind, workloadName, container.name, fieldPath),
			Analyzer:         a.Name(),
			Severity:         severity,
			Category:         category,
			Namespace:        namespace,
			Kind:             kind,
			Name:             workloadName,
			Container:        container.name,
			FieldPath:        fieldPath,
			CurrentValue:     currentValue,
			ExpectedValue:    expectedValue,
			Message:          message,
			Recommendation:   recommendation,
			DocumentationURL: documentationURL,
		})
	}

	if container.securityContext != nil && container.securityContext.Privileged != nil && *container.securityContext.Privileged {
		appendFinding(
			"WLK-PRIV-001", models.High, "security", ".securityContext.privileged", "true", "false",
			fmt.Sprintf("Container %q runs in privileged mode.", container.name),
			"Set securityContext.privileged to false and grant only the specific capabilities the container requires.",
			podSecurityStandardsURL,
		)
	}

	if !isWindows {
		allowPrivilegeEscalation := container.securityContext != nil && container.securityContext.AllowPrivilegeEscalation != nil && !*container.securityContext.AllowPrivilegeEscalation
		if !allowPrivilegeEscalation {
			current := "unset (defaults to true)"
			if container.securityContext != nil && container.securityContext.AllowPrivilegeEscalation != nil {
				current = fmt.Sprintf("%t", *container.securityContext.AllowPrivilegeEscalation)
			}
			appendFinding(
				"WLK-PRIVESC-001", models.Medium, "security", ".securityContext.allowPrivilegeEscalation", current, "false",
				fmt.Sprintf("Container %q does not prevent privilege escalation.", container.name),
				"Set securityContext.allowPrivilegeEscalation to false.",
				podSecurityStandardsURL,
			)
		}

		runAsNonRoot := podSecurityContext != nil && podSecurityContext.RunAsNonRoot != nil && *podSecurityContext.RunAsNonRoot
		current := "unset"
		if podSecurityContext != nil && podSecurityContext.RunAsNonRoot != nil {
			current = fmt.Sprintf("pod=%t", *podSecurityContext.RunAsNonRoot)
		}
		if container.securityContext != nil && container.securityContext.RunAsNonRoot != nil {
			runAsNonRoot = *container.securityContext.RunAsNonRoot
			current = fmt.Sprintf("container=%t", *container.securityContext.RunAsNonRoot)
		}
		if !runAsNonRoot {
			appendFinding(
				"WLK-NONROOT-001", models.Medium, "security", ".securityContext.runAsNonRoot", current, "true",
				fmt.Sprintf("Container %q does not require non-root execution.", container.name),
				"Set runAsNonRoot: true at pod or container security-context level and use a non-root image user.",
				podSecurityStandardsURL,
			)
		}
	}

	if !container.checkResources {
		return findings
	}

	if missing := missingResources(container.resources.Requests); len(missing) > 0 {
		appendFinding(
			"WLK-RSRC-REQ-001", models.Low, "resources", ".resources.requests", "missing: "+strings.Join(missing, ", "), "cpu and memory requests",
			fmt.Sprintf("Container %q is missing %s resource requests.", container.name, strings.Join(missing, " and ")),
			"Define CPU and memory requests from observed workload usage so the scheduler can place the pod reliably.",
			resourceManagementURL,
		)
	}
	if _, hasMemoryLimit := container.resources.Limits[corev1.ResourceMemory]; !hasMemoryLimit {
		appendFinding(
			"WLK-RSRC-LIMIT-001", models.Low, "resources", ".resources.limits.memory", "missing", "memory limit",
			fmt.Sprintf("Container %q is missing a memory limit.", container.name),
			"Set a memory limit based on measured peak usage; add a CPU limit only when throttling is appropriate for the workload.",
			resourceManagementURL,
		)
	}

	return findings
}

func missingResources(resources corev1.ResourceList) []string {
	missing := make([]string, 0, 2)
	if _, exists := resources[corev1.ResourceCPU]; !exists {
		missing = append(missing, string(corev1.ResourceCPU))
	}
	if _, exists := resources[corev1.ResourceMemory]; !exists {
		missing = append(missing, string(corev1.ResourceMemory))
	}
	return missing
}
