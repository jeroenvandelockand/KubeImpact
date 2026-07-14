package workload

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

func TestAnalyzeCoversWorkloadsAndProducesPreciseFindings(t *testing.T) {
	privileged := true
	allowEscalation := true
	runAsNonRoot := true
	automountToken := false
	readOnlyRoot := true
	seccomp := &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	secureResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
	}

	snapshot := &collector.Snapshot{
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "api"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				HostNetwork: true,
				Containers: []corev1.Container{{
					Name:      "api",
					Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
					SecurityContext: &corev1.SecurityContext{
						Privileged:               &privileged,
						AllowPrivilegeEscalation: &allowEscalation,
					},
				}},
			}}},
		}},
		StatefulSets: []appsv1.StatefulSet{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "data", Name: "database"},
			Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				AutomountServiceAccountToken: &automountToken,
				SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot, SeccompProfile: seccomp},
				Containers: []corev1.Container{{
					Name:      "database",
					Resources: secureResources,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: boolPointer(false),
						ReadOnlyRootFilesystem:   &readOnlyRoot,
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
				}},
			}}},
		}},
		DaemonSets: []appsv1.DaemonSet{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "agent"},
			Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				HostPID: true,
				InitContainers: []corev1.Container{{
					Name: "setup",
				}},
			}}},
		}},
	}

	result, err := New().Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	wanted := map[string]bool{
		"WLK-HOSTNET-001":    false,
		"WLK-PRIV-001":       false,
		"WLK-PRIVESC-001":    false,
		"WLK-NONROOT-001":    false,
		"WLK-RSRC-REQ-001":   false,
		"WLK-RSRC-LIMIT-001": false,
		"WLK-HOSTPID-001":    false,
	}
	fingerprints := make(map[string]struct{})
	for _, finding := range result.Findings {
		if _, exists := wanted[finding.ID]; exists {
			wanted[finding.ID] = true
		}
		if finding.Fingerprint == "" {
			t.Errorf("finding %s has no fingerprint", finding.ID)
		}
		if _, duplicate := fingerprints[finding.Fingerprint]; duplicate {
			t.Errorf("duplicate fingerprint %q", finding.Fingerprint)
		}
		fingerprints[finding.Fingerprint] = struct{}{}
		if finding.Container != "" && finding.FieldPath == "" {
			t.Errorf("container finding %s has no field path", finding.ID)
		}
	}
	for id, found := range wanted {
		if !found {
			t.Errorf("expected finding %s", id)
		}
	}

	for _, finding := range result.Findings {
		if finding.Name == "database" {
			t.Errorf("secure StatefulSet unexpectedly produced %s", finding.ID)
		}
	}
}

func TestAnalyzeRejectsNilAndCanceledContext(t *testing.T) {
	if _, err := New().Analyze(context.Background(), nil); err == nil {
		t.Fatal("Analyze(nil) returned no error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Analyze(ctx, &collector.Snapshot{Deployments: []appsv1.Deployment{{}}}); err == nil {
		t.Fatal("Analyze() with canceled context returned no error")
	}
}

func TestMissingResources(t *testing.T) {
	complete := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}
	if missing := missingResources(complete); len(missing) != 0 {
		t.Fatalf("missingResources(complete) = %v", missing)
	}
	if missing := missingResources(corev1.ResourceList{}); len(missing) != 2 {
		t.Fatalf("missingResources(empty) = %v", missing)
	}
}

func TestCPUlimitIsOptionalAndEphemeralResourcesAreNotRequired(t *testing.T) {
	runAsNonRoot := true
	allowEscalation := false
	automountToken := false
	readOnlyRoot := true
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("64Mi"),
	}
	memoryLimit := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}
	snapshot := &collector.Snapshot{Deployments: []appsv1.Deployment{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "safe"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &automountToken,
			SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{
				Name:            "app",
				Resources:       corev1.ResourceRequirements{Requests: requests, Limits: memoryLimit},
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowEscalation, ReadOnlyRootFilesystem: &readOnlyRoot, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			}},
			EphemeralContainers: []corev1.EphemeralContainer{{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:            "debugger",
					SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowEscalation, ReadOnlyRootFilesystem: &readOnlyRoot, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
				},
			}},
		}}},
	}}}

	result, err := New().Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("Findings = %#v, want none", result.Findings)
	}
}

func boolPointer(value bool) *bool { return &value }

func TestBaselineProfileAndAuditedSuppressions(t *testing.T) {
	privileged := true
	baseSnapshot := func(annotations map[string]string) *collector.Snapshot {
		return &collector.Snapshot{Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "api", Annotations: annotations},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "api", SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
			}}}}},
		}}}
	}
	config := policy.Config{Profile: policy.ProfileBaseline, SeverityOverrides: map[string]models.Severity{"WLK-PRIV-001": models.Critical}}
	result, err := New(config).Analyze(context.Background(), baseSnapshot(map[string]string{
		ignoreAnnotation: "WLK-PRIV-001", ignoreReasonAnnotation: "Vendor node agent requires privileged access; reviewed by platform team.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 || len(result.Suppressions) != 1 || result.Suppressions[0].Reason == "" {
		t.Fatalf("result = %#v", result)
	}

	result, err = New(config).Analyze(context.Background(), baseSnapshot(map[string]string{ignoreAnnotation: "WLK-PRIV-001"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 2 || result.Findings[0].ID != "WLK-SUPPRESSION-INVALID-001" || result.Findings[1].Severity != models.Critical {
		t.Fatalf("missing-reason findings = %#v", result.Findings)
	}
}

func TestBaselineProfileSkipsRestrictedRulesAndHonorsExclusions(t *testing.T) {
	snapshot := &collector.Snapshot{Deployments: []appsv1.Deployment{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "excluded", Name: "api"},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}}}},
	}}}
	config := policy.Config{Profile: policy.ProfileBaseline, Exclusions: policy.Exclusions{Namespaces: []string{"excluded"}}}
	result, err := New(config).Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("Findings = %#v", result.Findings)
	}
}

func TestNewBaselineRules(t *testing.T) {
	snapshot := &collector.Snapshot{Deployments: []appsv1.Deployment{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "risky"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Volumes:         []corev1.Volume{{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}},
			SecurityContext: &corev1.PodSecurityContext{Sysctls: []corev1.Sysctl{{Name: "kernel.core_pattern", Value: "core"}}},
			Containers: []corev1.Container{{
				Name: "api", Ports: []corev1.ContainerPort{{HostPort: 8080}},
				SecurityContext: &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}}},
			}},
		}}},
	}}}
	result, err := New(policy.Config{Profile: policy.ProfileBaseline}).Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"WLK-HOSTPATH-001": false, "WLK-HOSTPORT-001": false, "WLK-SYSCTL-001": false, "WLK-CAPABILITIES-BASELINE-001": false}
	for _, finding := range result.Findings {
		wanted[finding.ID] = true
	}
	for id, found := range wanted {
		if !found {
			t.Errorf("missing %s", id)
		}
	}
}

func TestRestrictedCapabilitiesOnlyAllowNetBindService(t *testing.T) {
	target := workloadTarget{podSpec: &corev1.PodSpec{}}
	container := containerTarget{
		name: "api", fieldPath: "spec.template.spec.containers[api]",
		securityContext: &corev1.SecurityContext{Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"CHOWN"},
		}},
	}
	violations := capabilityViolations(target, container)
	if len(violations) != 1 || violations[0].id != "WLK-CAPABILITIES-RESTRICTED-001" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestNamespaceLabelExclusionsStayWithinEvidenceSource(t *testing.T) {
	snapshot := &collector.Snapshot{
		Namespaces: []corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{
			Name: "apps", Labels: map[string]string{"excluded": "true"},
			Annotations: map[string]string{collector.SourceAnnotation: "directory:a#namespace.yaml", collector.SourceScopeAnnotation: "directory:a"},
		}}},
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps", Annotations: map[string]string{
				collector.SourceAnnotation: "directory:b#deployment.yaml", collector.SourceScopeAnnotation: "directory:b",
			}},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{HostNetwork: true}}},
		}},
	}
	config := policy.Config{Profile: policy.ProfileBaseline, Exclusions: policy.Exclusions{NamespaceLabels: map[string]string{"excluded": "true"}}}
	result, err := New(config).Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].ID != "WLK-HOSTNET-001" {
		t.Fatalf("Findings = %#v", result.Findings)
	}
}
