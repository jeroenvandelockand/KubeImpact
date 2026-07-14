package workload

import (
	corev1 "k8s.io/api/core/v1"

	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

const (
	podSecurityStandardsURL = "https://kubernetes.io/docs/concepts/security/pod-security-standards/"
	resourceManagementURL   = "https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/"
	serviceAccountsURL      = "https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/"
)

type workloadTarget struct {
	kind        string
	namespace   string
	name        string
	labels      map[string]string
	annotations map[string]string
	podSpec     *corev1.PodSpec
	source      string
	sourceScope string
}

type containerTarget struct {
	name            string
	fieldPath       string
	resources       corev1.ResourceRequirements
	securityContext *corev1.SecurityContext
	ports           []corev1.ContainerPort
	checkResources  bool
}

type violation struct {
	id               string
	severity         models.Severity
	category         string
	container        string
	fieldPath        string
	currentValue     string
	expectedValue    string
	message          string
	recommendation   string
	documentationURL string
}

type podRule struct {
	profile  policy.Profile
	evaluate func(workloadTarget) []violation
}

type containerRule struct {
	profile  policy.Profile
	evaluate func(workloadTarget, containerTarget) []violation
}

func containers(podSpec *corev1.PodSpec) []containerTarget {
	result := make([]containerTarget, 0, len(podSpec.Containers)+len(podSpec.InitContainers)+len(podSpec.EphemeralContainers))
	for i := range podSpec.Containers {
		container := &podSpec.Containers[i]
		result = append(result, containerTarget{
			name: container.Name, fieldPath: "spec.template.spec.containers[" + container.Name + "]",
			resources: container.Resources, securityContext: container.SecurityContext, ports: container.Ports, checkResources: true,
		})
	}
	for i := range podSpec.InitContainers {
		container := &podSpec.InitContainers[i]
		result = append(result, containerTarget{
			name: container.Name, fieldPath: "spec.template.spec.initContainers[" + container.Name + "]",
			resources: container.Resources, securityContext: container.SecurityContext, ports: container.Ports, checkResources: true,
		})
	}
	for i := range podSpec.EphemeralContainers {
		container := &podSpec.EphemeralContainers[i]
		result = append(result, containerTarget{
			name: container.Name, fieldPath: "spec.template.spec.ephemeralContainers[" + container.Name + "]",
			resources: container.Resources, securityContext: container.SecurityContext, ports: container.Ports,
		})
	}
	return result
}

func isWindows(podSpec *corev1.PodSpec) bool {
	return podSpec.OS != nil && podSpec.OS.Name == corev1.Windows ||
		podSpec.NodeSelector[corev1.LabelOSStable] == string(corev1.Windows) ||
		podSpec.NodeSelector["beta.kubernetes.io/os"] == string(corev1.Windows)
}
