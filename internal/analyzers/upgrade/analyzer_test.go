package upgrade

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

func TestAnalyzeLoadsTheCompleteUpgradePath(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.34.9",
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "edge", Name: "legacy"},
			Spec:       corev1.ServiceSpec{ExternalIPs: []string{"203.0.113.10"}},
		}},
		Resources: []models.KubernetesResource{{
			Kind:                "StorageVersionMigration",
			Name:                "migration",
			ObservedAPIVersions: []string{"storagemigration.k8s.io/v1alpha1"},
		}},
	}

	result, err := New("1.36").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	wanted := map[string]bool{
		"UPG-1.35-STORAGEMIGRATION-V1ALPHA1": false,
		"UPG-1.36-SERVICE-EXTERNALIPS":       false,
	}
	for _, impact := range result.UpgradeImpact {
		wanted[impact.Rule] = true
		if impact.Fingerprint == "" || impact.DocumentationURL == "" {
			t.Errorf("impact %s is missing evidence metadata", impact.Rule)
		}
	}
	for rule, found := range wanted {
		if !found {
			t.Errorf("expected impact %s", rule)
		}
	}
}

func TestAnalyzeDoesNotInferAPIUsageWithoutManagedFieldsEvidence(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.34.0",
		Resources: []models.KubernetesResource{{
			Kind: "StorageVersionMigration",
			Name: "migration",
		}},
	}
	result, err := New("1.35").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(result.UpgradeImpact) != 0 {
		t.Fatalf("UpgradeImpact = %#v, want none", result.UpgradeImpact)
	}
}

func TestAnalyzeMatchesRequiredLiveInventoryByListedVersion(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Resources: []models.KubernetesResource{{
			Kind: "Workload", Namespace: "default", Name: "training", Source: "cluster",
			ListedAPIVersion: "scheduling.k8s.io/v1alpha2",
		}},
	}
	result, err := New("1.37").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UpgradeImpact) != 1 || result.UpgradeImpact[0].Rule != "UPG-1.37-WORKLOAD-V1ALPHA2" {
		t.Fatalf("UpgradeImpact = %#v", result.UpgradeImpact)
	}
	if result.UpgradeImpact[0].FieldPath != "listedApiVersion" {
		t.Fatalf("FieldPath = %q, want listedApiVersion", result.UpgradeImpact[0].FieldPath)
	}
}

func TestAnalyzeRejectsNonUpgradeTarget(t *testing.T) {
	_, err := New("1.35").Analyze(context.Background(), &collector.Snapshot{ClusterVersion: "v1.35.2"})
	if err == nil {
		t.Fatal("Analyze() returned no error")
	}
}

func TestAnalyzeUsesAPIServerRequestMetrics(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.34.0",
		DeprecatedAPIRequests: []models.DeprecatedAPIRequest{{
			GroupVersion: "extensions/v1beta1", Resource: "ingresses", RemovedRelease: "1.35",
		}, {
			GroupVersion: "example.io/v1alpha1", Resource: "widgets", RemovedRelease: "1.37",
		}},
	}
	result, err := New("1.36").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UpgradeImpact) != 1 || result.UpgradeImpact[0].Rule != "UPG-API-REQUEST-001" || result.UpgradeImpact[0].Source != "apiserver-metrics" {
		t.Fatalf("UpgradeImpact = %#v", result.UpgradeImpact)
	}
}

func TestAnalyzeIgnoresSourceOnlyRulesForClusterInventory(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Resources: []models.KubernetesResource{{
			Kind: "InitConfiguration", Name: "InitConfiguration", Source: "cluster",
			ObservedAPIVersions: []string{"kubeadm.k8s.io/v1beta3"},
		}},
	}
	result, err := New("1.37").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UpgradeImpact) != 0 {
		t.Fatalf("UpgradeImpact = %#v, want none", result.UpgradeImpact)
	}
}

func TestAnalyzeDoesNotMistakeCoreDNSCompatibilityLabelForKubeDNS(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Deployments: []appsv1.Deployment{{ObjectMeta: metav1.ObjectMeta{
			Name: "coredns", Namespace: "kube-system", Labels: map[string]string{"k8s-app": "kube-dns"},
		}}},
	}
	result, err := New("1.37").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, impact := range result.UpgradeImpact {
		if impact.Rule == "UPG-1.37-KUBEDNS" {
			t.Fatalf("CoreDNS was reported as kube-dns: %#v", impact)
		}
	}
}

func TestStaticPodLegacyVolumeSecretIsAnAPIReference(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Pods: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "static-storage", Namespace: "kube-system",
				Annotations: map[string]string{"kubernetes.io/config.source": "file"},
			},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: "legacy",
				VolumeSource: corev1.VolumeSource{RBD: &corev1.RBDVolumeSource{
					RBDImage: "image", SecretRef: &corev1.LocalObjectReference{Name: "ceph-secret"},
				}},
			}}},
		}},
	}
	result, err := New("1.37").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-STATIC-POD-API-REFERENCE")
	if len(impacts) != 1 || impacts[0].FieldPath != "spec.volumes[0].rbd.secretRef" {
		t.Fatalf("static Pod secret impacts = %#v", impacts)
	}
}

func TestUnannotatedPodIsNotAssumedToBeStatic(t *testing.T) {
	snapshot := &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Pods: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "ordinary", Namespace: "default"},
			Spec:       corev1.PodSpec{ServiceAccountName: "application"},
		}},
	}
	result, err := New("1.37").Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-STATIC-POD-API-REFERENCE"); len(impacts) != 0 {
		t.Fatalf("unannotated Pod was treated as static: %#v", impacts)
	}
}

func TestAnalyzeDetectsCloudControllerManagerNodeMonitorPeriod(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "CloudControllerManagerConfiguration", Name: "CloudControllerManagerConfiguration", Source: "directory:cloud-controller-manager.yaml",
		Object: map[string]any{
			"kubeCloudShared": map[string]any{"nodeMonitorPeriod": "5s"},
		},
	}})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-CLOUD-NODE-MONITOR-PERIOD")
	if len(impacts) != 1 {
		t.Fatalf("cloud node-monitor impacts = %#v", impacts)
	}
	if impacts[0].FieldPath != "kubeCloudShared.nodeMonitorPeriod" || impacts[0].ExpectedValue != "nodeLifecycleController.nodeMonitorPeriod" {
		t.Fatalf("cloud node-monitor impact = %#v", impacts[0])
	}
}

func TestAnalyzeDetectsRemovedFeatureGateInComponentConfiguration(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "KubeletConfiguration", Name: "KubeletConfiguration", Source: "directory:kubelet.yaml",
		Object: map[string]any{
			"apiVersion": "kubelet.config.k8s.io/v1beta1",
			"featureGates": map[string]any{
				"SidecarContainers": true,
				"CurrentFeature":    true,
			},
		},
	}})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-FEATURE-GATE")
	if len(impacts) != 1 {
		t.Fatalf("removed feature-gate impacts = %#v", impacts)
	}
	if impacts[0].FieldPath != "featureGates[SidecarContainers]" || impacts[0].CurrentValue != "SidecarContainers=true" {
		t.Fatalf("removed feature-gate impact = %#v", impacts[0])
	}
}

func TestAnalyzeDetectsFeatureGatesSetAgainstKubernetes137LockedDefaults(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{
		{
			Kind: "ClusterConfiguration", Name: "ClusterConfiguration", Source: "directory:kubeadm.yaml",
			Object: map[string]any{
				"apiVersion": "kubeadm.k8s.io/v1beta4",
				"apiServer": map[string]any{
					"extraArgs": map[string]any{
						"feature-gates": "DeclarativeValidationTakeover=true,DRAPrioritizedList=false,HostnameOverride=false",
					},
				},
			},
		},
		{
			Kind: "KubeletConfiguration", Name: "KubeletConfiguration", Source: "directory:kubelet.yaml",
			Object: map[string]any{
				"apiVersion": "kubelet.config.k8s.io/v1beta1",
				"featureGates": map[string]any{
					"DeclarativeValidationTakeover": false,
					"DRAPrioritizedList":            true,
					"HostnameOverride":              true,
				},
			},
		},
	})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-LOCKED-FEATURE-GATE")
	got := make(map[string]string, len(impacts))
	for _, impact := range impacts {
		got[impact.CurrentValue] = impact.ExpectedValue
	}
	wanted := map[string]string{
		"DeclarativeValidationTakeover=true": "DeclarativeValidationTakeover=false",
		"DRAPrioritizedList=false":           "DRAPrioritizedList=true",
		"HostnameOverride=false":             "HostnameOverride=true",
	}
	if len(got) != len(wanted) {
		t.Fatalf("locked feature-gate impacts = %#v", impacts)
	}
	for current, expected := range wanted {
		if got[current] != expected {
			t.Errorf("locked feature gate %s expected %q, got %q", current, expected, got[current])
		}
	}
}

func TestAnalyzeDoesNotTreatUnrelatedConfigurationAsKubernetesComponent(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "WidgetConfiguration", Name: "widget", Source: "directory:widget.yaml",
		Object: map[string]any{
			"featureGates": map[string]any{"SidecarContainers": true},
			"extraArgs":    map[string]any{"container-hints": "true"},
		},
	}})
	if impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-FEATURE-GATE"); len(impacts) != 0 {
		t.Fatalf("unrelated configuration feature-gate impacts = %#v", impacts)
	}
	if impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-COMPONENT-FLAG"); len(impacts) != 0 {
		t.Fatalf("unrelated configuration flag impacts = %#v", impacts)
	}
}

func TestAnalyzeDetectsRemovedFlagsInKubeadmExtraArgs(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{
		{
			Kind: "InitConfiguration", Name: "InitConfiguration", Source: "directory:kubeadm-init.yaml",
			Object: map[string]any{
				"apiVersion": "kubeadm.k8s.io/v1beta4",
				"nodeRegistration": map[string]any{
					"kubeletExtraArgs": map[string]any{
						"container-hints":              "/etc/cadvisor/container_hints.json",
						"global-housekeeping-interval": "1m",
						"housekeeping-interval":        "10s",
					},
				},
			},
		},
		{
			Kind: "ClusterConfiguration", Name: "ClusterConfiguration", Source: "directory:kubeadm-cluster.yaml",
			Object: map[string]any{
				"apiVersion": "kubeadm.k8s.io/v1beta4",
				"controllerManager": map[string]any{
					"extraArgs": map[string]any{
						"concurrent-service-syncs":    "2",
						"concurrent-deployment-syncs": "5",
					},
				},
			},
		},
	})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-COMPONENT-FLAG")
	got := make(map[string]bool, len(impacts))
	for _, impact := range impacts {
		got[impact.CurrentValue] = true
	}
	for _, flag := range []string{"--container-hints", "--global-housekeeping-interval", "--concurrent-service-syncs"} {
		if !got[flag] {
			t.Errorf("missing %s impact in %#v", flag, impacts)
		}
	}
	if len(impacts) != 3 {
		t.Fatalf("removed component-flag impacts = %#v", impacts)
	}
}

func TestAnalyzeDetectsRemovedFeatureGateInSplitContainerArguments(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "Pod", Namespace: "kube-system", Name: "kube-apiserver-control-plane", Source: "directory:manifests/kube-apiserver.yaml",
		Object: map[string]any{
			"apiVersion": "v1",
			"spec": map[string]any{
				"containers": []any{map[string]any{
					"name": "kube-apiserver",
					"args": []any{"kube-apiserver", "--feature-gates", "SidecarContainers=true", "--secure-port=6443"},
				}},
			},
		},
	}})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-FEATURE-GATE")
	if len(impacts) != 1 || impacts[0].CurrentValue != "SidecarContainers=true" {
		t.Fatalf("split feature-gate impacts = %#v", impacts)
	}
}

func TestAnalyzeOnlyReadsComponentSettingsFromSupportedPaths(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{
		{
			Kind: "KubeletConfiguration", Name: "wrong-group", Source: "directory:wrong.yaml",
			Object: map[string]any{
				"apiVersion":   "example.io/v1",
				"featureGates": map[string]any{"SidecarContainers": true},
			},
		},
		{
			Kind: "KubeletConfiguration", Name: "nested", Source: "directory:nested.yaml",
			Object: map[string]any{
				"apiVersion": "kubelet.config.k8s.io/v1beta1",
				"metadata": map[string]any{
					"annotations": map[string]any{"featureGates": map[string]any{"SidecarContainers": true}},
				},
			},
		},
		{
			Kind: "ClusterConfiguration", Name: "wrong-component", Source: "directory:kubeadm.yaml",
			Object: map[string]any{
				"apiVersion": "kubeadm.k8s.io/v1beta4",
				"apiServer": map[string]any{
					"extraArgs": map[string]any{"container-hints": "true"},
				},
			},
		},
		{
			Kind: "Pod", Name: "kubelet-looking-name", Source: "directory:pod.yaml",
			Object: map[string]any{
				"apiVersion": "v1",
				"spec": map[string]any{"containers": []any{map[string]any{
					"name": "business-app", "args": []any{"--container-hints=true", "--feature-gates=SidecarContainers=true"},
				}}},
			},
		},
	})

	if impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-FEATURE-GATE"); len(impacts) != 0 {
		t.Fatalf("unsupported-path feature-gate impacts = %#v", impacts)
	}
	if impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-COMPONENT-FLAG"); len(impacts) != 0 {
		t.Fatalf("unsupported-path flag impacts = %#v", impacts)
	}
}

func TestAnalyzeTiesRemovedFlagsToTheirOwningComponent(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "Pod", Namespace: "kube-system", Name: "components", Source: "directory:components.yaml",
		Object: map[string]any{
			"apiVersion": "v1",
			"spec": map[string]any{"containers": []any{
				map[string]any{"name": "kubelet", "args": []any{"--container-hints=true", "--concurrent-service-syncs=2"}},
				map[string]any{"name": "kube-controller-manager", "args": []any{"--concurrent-service-syncs=2", "--container-hints=true"}},
			}},
		},
	}})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-COMPONENT-FLAG")
	if len(impacts) != 2 {
		t.Fatalf("component-owned flag impacts = %#v", impacts)
	}
	got := map[string]bool{}
	for _, impact := range impacts {
		got[impact.CurrentValue] = true
	}
	if !got["--container-hints"] || !got["--concurrent-service-syncs"] {
		t.Fatalf("component-owned flag impacts = %#v", impacts)
	}
}

func TestAnalyzeReadsLiveComponentArgumentsWithoutTypeMeta(t *testing.T) {
	result := analyzeKubernetes137(t, []models.KubernetesResource{{
		Kind: "DaemonSet", Namespace: "kube-system", Name: "node-agent", Source: "cluster",
		Object: map[string]any{
			"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{
					"name": "kubelet", "args": []any{"--container-hints=true"},
				}},
			}}},
		},
	}})

	impacts := impactsForRule(result.UpgradeImpact, "UPG-1.37-REMOVED-COMPONENT-FLAG")
	if len(impacts) != 1 || impacts[0].CurrentValue != "--container-hints" {
		t.Fatalf("live component flag impacts = %#v", impacts)
	}
}

func analyzeKubernetes137(t *testing.T, resources []models.KubernetesResource) *models.AnalysisResult {
	t.Helper()
	result, err := New("1.37").Analyze(context.Background(), &collector.Snapshot{
		ClusterVersion: "v1.36.0",
		Resources:      resources,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func impactsForRule(impacts []models.UpgradeImpact, rule string) []models.UpgradeImpact {
	result := make([]models.UpgradeImpact, 0)
	for _, impact := range impacts {
		if impact.Rule == rule {
			result = append(result, impact)
		}
	}
	return result
}
