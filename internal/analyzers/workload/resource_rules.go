package workload

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"kubeimpact/internal/models"
)

func resourceViolations(_ workloadTarget, container containerTarget) []violation {
	if !container.checkResources {
		return nil
	}
	var result []violation
	if missing := missingResources(container.resources.Requests); len(missing) > 0 {
		result = append(result, violation{id: "WLK-RSRC-REQ-001", severity: models.Low, category: "resources", container: container.name, fieldPath: container.fieldPath + ".resources.requests", currentValue: "missing: " + strings.Join(missing, ", "), expectedValue: "cpu and memory requests", message: fmt.Sprintf("Container %q is missing %s resource requests.", container.name, strings.Join(missing, " and ")), recommendation: "Define CPU and memory requests from observed workload usage so the scheduler can place the pod reliably.", documentationURL: resourceManagementURL})
	}
	if _, exists := container.resources.Limits[corev1.ResourceMemory]; !exists {
		result = append(result, violation{id: "WLK-RSRC-LIMIT-001", severity: models.Low, category: "resources", container: container.name, fieldPath: container.fieldPath + ".resources.limits.memory", currentValue: "missing", expectedValue: "memory limit", message: fmt.Sprintf("Container %q is missing a memory limit.", container.name), recommendation: "Set a memory limit based on measured peak usage; add a CPU limit only when throttling is appropriate for the workload.", documentationURL: resourceManagementURL})
	}
	return result
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
