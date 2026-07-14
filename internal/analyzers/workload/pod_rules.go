package workload

import (
	"fmt"
	"strings"

	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

func podRules() []podRule {
	return []podRule{
		{profile: policy.ProfileBaseline, evaluate: hostNamespaceViolations},
		{profile: policy.ProfileBaseline, evaluate: hostPathViolations},
		{profile: policy.ProfileBaseline, evaluate: unsafeSysctlViolations},
		{profile: policy.ProfileRestricted, evaluate: serviceAccountTokenViolations},
	}
}

func hostNamespaceViolations(target workloadTarget) []violation {
	var result []violation
	checks := []struct {
		enabled        bool
		id             string
		severity       models.Severity
		field          string
		message        string
		recommendation string
	}{
		{target.podSpec.HostNetwork, "WLK-HOSTNET-001", models.Medium, "spec.template.spec.hostNetwork", "Pod uses the node network namespace.", "Remove hostNetwork: true unless node-network access is required; document and constrain any exception."},
		{target.podSpec.HostPID, "WLK-HOSTPID-001", models.High, "spec.template.spec.hostPID", "Pod shares the node process ID namespace.", "Remove hostPID: true unless node process access is strictly required."},
		{target.podSpec.HostIPC, "WLK-HOSTIPC-001", models.High, "spec.template.spec.hostIPC", "Pod shares the node IPC namespace.", "Remove hostIPC: true unless node IPC access is strictly required."},
	}
	for _, check := range checks {
		if check.enabled {
			result = append(result, violation{id: check.id, severity: check.severity, category: "security", fieldPath: check.field, currentValue: "true", expectedValue: "false", message: check.message, recommendation: check.recommendation, documentationURL: podSecurityStandardsURL})
		}
	}
	return result
}

func hostPathViolations(target workloadTarget) []violation {
	var result []violation
	for _, volume := range target.podSpec.Volumes {
		if volume.HostPath == nil {
			continue
		}
		result = append(result, violation{
			id: "WLK-HOSTPATH-001", severity: models.High, category: "security",
			fieldPath: "spec.template.spec.volumes[" + volume.Name + "].hostPath", currentValue: volume.HostPath.Path, expectedValue: "a non-hostPath volume",
			message:        fmt.Sprintf("Volume %q mounts a path from the node filesystem.", volume.Name),
			recommendation: "Replace hostPath with a purpose-built volume. If node access is unavoidable, constrain the path and mount it read-only.", documentationURL: podSecurityStandardsURL,
		})
	}
	return result
}

var safeSysctls = map[string]bool{
	"kernel.shm_rmid_forced":              true,
	"net.ipv4.ip_local_port_range":        true,
	"net.ipv4.ip_unprivileged_port_start": true,
	"net.ipv4.tcp_syncookies":             true,
	"net.ipv4.ip_local_reserved_ports":    true,
	"net.ipv4.tcp_keepalive_time":         true,
	"net.ipv4.tcp_fin_timeout":            true,
	"net.ipv4.tcp_keepalive_intvl":        true,
	"net.ipv4.tcp_keepalive_probes":       true,
}

func unsafeSysctlViolations(target workloadTarget) []violation {
	if target.podSpec.SecurityContext == nil || isWindows(target.podSpec) {
		return nil
	}
	var result []violation
	for _, sysctl := range target.podSpec.SecurityContext.Sysctls {
		if safeSysctls[sysctl.Name] {
			continue
		}
		result = append(result, violation{
			id: "WLK-SYSCTL-001", severity: models.High, category: "security",
			fieldPath: "spec.template.spec.securityContext.sysctls[" + sysctl.Name + "]", currentValue: sysctl.Name + "=" + sysctl.Value, expectedValue: "a safe sysctl",
			message: fmt.Sprintf("Pod configures unsafe sysctl %q.", sysctl.Name), recommendation: "Remove the unsafe sysctl or isolate the workload on dedicated nodes with an explicitly reviewed policy.", documentationURL: podSecurityStandardsURL,
		})
	}
	return result
}

func serviceAccountTokenViolations(target workloadTarget) []violation {
	if target.podSpec.AutomountServiceAccountToken != nil && !*target.podSpec.AutomountServiceAccountToken {
		return nil
	}
	current := "unset (defaults to true)"
	if target.podSpec.AutomountServiceAccountToken != nil {
		current = fmt.Sprintf("%t", *target.podSpec.AutomountServiceAccountToken)
	}
	serviceAccount := strings.TrimSpace(target.podSpec.ServiceAccountName)
	if serviceAccount == "" {
		serviceAccount = "default"
	}
	return []violation{{
		id: "WLK-SA-TOKEN-001", severity: models.Low, category: "security", fieldPath: "spec.template.spec.automountServiceAccountToken", currentValue: current, expectedValue: "false unless Kubernetes API access is required",
		message: fmt.Sprintf("Pod automatically mounts credentials for ServiceAccount %q.", serviceAccount), recommendation: "Set automountServiceAccountToken: false unless the workload calls the Kubernetes API; use a dedicated least-privilege ServiceAccount when it does.", documentationURL: serviceAccountsURL,
	}}
}
