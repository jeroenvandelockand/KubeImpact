package workload

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
)

func TestAnalyzeCoversWorkloadsAndProducesPreciseFindings(t *testing.T) {
	privileged := true
	allowEscalation := true
	runAsNonRoot := true
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
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
				Containers: []corev1.Container{{
					Name:      "database",
					Resources: secureResources,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: boolPointer(false),
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
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("64Mi"),
	}
	memoryLimit := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}
	snapshot := &collector.Snapshot{Deployments: []appsv1.Deployment{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "safe"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
			Containers: []corev1.Container{{
				Name:            "app",
				Resources:       corev1.ResourceRequirements{Requests: requests, Limits: memoryLimit},
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowEscalation},
			}},
			EphemeralContainers: []corev1.EphemeralContainer{{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:            "debugger",
					SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &allowEscalation},
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
