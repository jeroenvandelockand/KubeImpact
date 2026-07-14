package workload

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"kubeimpact/internal/models"
	"kubeimpact/internal/policy"
)

func containerRules() []containerRule {
	return []containerRule{
		{profile: policy.ProfileBaseline, evaluate: privilegedViolations},
		{profile: policy.ProfileBaseline, evaluate: hostPortViolations},
		{profile: policy.ProfileBaseline, evaluate: hostProcessViolations},
		{profile: policy.ProfileBaseline, evaluate: baselineCapabilityViolations},
		{profile: policy.ProfileRestricted, evaluate: privilegeEscalationViolations},
		{profile: policy.ProfileRestricted, evaluate: nonRootViolations},
		{profile: policy.ProfileRestricted, evaluate: capabilityViolations},
		{profile: policy.ProfileRestricted, evaluate: seccompViolations},
		{profile: policy.ProfileRestricted, evaluate: readOnlyFilesystemViolations},
		{profile: policy.ProfileRestricted, evaluate: resourceViolations},
	}
}

var baselineCapabilities = map[string]bool{
	"AUDIT_WRITE": true, "CHOWN": true, "DAC_OVERRIDE": true, "FOWNER": true, "FSETID": true,
	"KILL": true, "MKNOD": true, "NET_BIND_SERVICE": true, "SETFCAP": true, "SETGID": true,
	"SETPCAP": true, "SETUID": true, "SYS_CHROOT": true,
}

func baselineCapabilityViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) || container.securityContext == nil || container.securityContext.Capabilities == nil {
		return nil
	}
	var prohibited []string
	for _, capability := range container.securityContext.Capabilities.Add {
		name := strings.ToUpper(string(capability))
		if !baselineCapabilities[name] {
			prohibited = append(prohibited, name)
		}
	}
	if len(prohibited) == 0 {
		return nil
	}
	slices.Sort(prohibited)
	return []violation{{
		id: "WLK-CAPABILITIES-BASELINE-001", severity: models.High, category: "security", container: container.name,
		fieldPath: container.fieldPath + ".securityContext.capabilities.add", currentValue: strings.Join(prohibited, ", "), expectedValue: "only baseline-allowed capabilities",
		message: fmt.Sprintf("Container %q adds capabilities prohibited by the baseline Pod Security Standard.", container.name), recommendation: "Remove prohibited added capabilities and grant only the minimum baseline-allowed capabilities required by the process.", documentationURL: podSecurityStandardsURL,
	}}
}

func privilegedViolations(_ workloadTarget, container containerTarget) []violation {
	if container.securityContext == nil || container.securityContext.Privileged == nil || !*container.securityContext.Privileged {
		return nil
	}
	return []violation{{id: "WLK-PRIV-001", severity: models.High, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.privileged", currentValue: "true", expectedValue: "false", message: fmt.Sprintf("Container %q runs in privileged mode.", container.name), recommendation: "Set securityContext.privileged to false and grant only the specific capabilities the container requires.", documentationURL: podSecurityStandardsURL}}
}

func hostPortViolations(_ workloadTarget, container containerTarget) []violation {
	var result []violation
	for index, port := range container.ports {
		if port.HostPort == 0 {
			continue
		}
		identifier := port.Name
		if identifier == "" {
			identifier = fmt.Sprintf("%d", index)
		}
		result = append(result, violation{id: "WLK-HOSTPORT-001", severity: models.Medium, category: "network", container: container.name, fieldPath: container.fieldPath + ".ports[" + identifier + "].hostPort", currentValue: fmt.Sprintf("%d", port.HostPort), expectedValue: "0", message: fmt.Sprintf("Container %q binds node port %d directly.", container.name, port.HostPort), recommendation: "Remove hostPort and expose the workload through a Service unless direct node binding is required.", documentationURL: podSecurityStandardsURL})
	}
	return result
}

func hostProcessViolations(target workloadTarget, container containerTarget) []violation {
	if !isWindows(target.podSpec) || container.securityContext == nil || container.securityContext.WindowsOptions == nil || container.securityContext.WindowsOptions.HostProcess == nil || !*container.securityContext.WindowsOptions.HostProcess {
		return nil
	}
	return []violation{{id: "WLK-HOSTPROCESS-001", severity: models.High, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.windowsOptions.hostProcess", currentValue: "true", expectedValue: "false", message: fmt.Sprintf("Windows container %q runs as a HostProcess container.", container.name), recommendation: "Disable HostProcess unless the workload is an explicitly reviewed node-management component.", documentationURL: podSecurityStandardsURL}}
}

func privilegeEscalationViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) {
		return nil
	}
	if container.securityContext != nil && container.securityContext.AllowPrivilegeEscalation != nil && !*container.securityContext.AllowPrivilegeEscalation {
		return nil
	}
	current := "unset (defaults to true)"
	if container.securityContext != nil && container.securityContext.AllowPrivilegeEscalation != nil {
		current = fmt.Sprintf("%t", *container.securityContext.AllowPrivilegeEscalation)
	}
	return []violation{{id: "WLK-PRIVESC-001", severity: models.Medium, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.allowPrivilegeEscalation", currentValue: current, expectedValue: "false", message: fmt.Sprintf("Container %q does not prevent privilege escalation.", container.name), recommendation: "Set securityContext.allowPrivilegeEscalation to false.", documentationURL: podSecurityStandardsURL}}
}

func nonRootViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) {
		return nil
	}
	runAsNonRoot := target.podSpec.SecurityContext != nil && target.podSpec.SecurityContext.RunAsNonRoot != nil && *target.podSpec.SecurityContext.RunAsNonRoot
	current := "unset"
	if target.podSpec.SecurityContext != nil && target.podSpec.SecurityContext.RunAsNonRoot != nil {
		current = fmt.Sprintf("pod=%t", *target.podSpec.SecurityContext.RunAsNonRoot)
	}
	if container.securityContext != nil && container.securityContext.RunAsNonRoot != nil {
		runAsNonRoot = *container.securityContext.RunAsNonRoot
		current = fmt.Sprintf("container=%t", *container.securityContext.RunAsNonRoot)
	}
	if runAsNonRoot {
		return nil
	}
	return []violation{{id: "WLK-NONROOT-001", severity: models.Medium, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.runAsNonRoot", currentValue: current, expectedValue: "true", message: fmt.Sprintf("Container %q does not require non-root execution.", container.name), recommendation: "Set runAsNonRoot: true at pod or container level and use a non-root image user.", documentationURL: podSecurityStandardsURL}}
}

func capabilityViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) {
		return nil
	}
	dropped := []corev1.Capability{}
	if container.securityContext != nil && container.securityContext.Capabilities != nil {
		dropped = container.securityContext.Capabilities.Drop
	}
	var result []violation
	values := make([]string, len(dropped))
	for i, value := range dropped {
		values[i] = string(value)
	}
	current := "none"
	if len(values) > 0 {
		current = strings.Join(values, ", ")
	}
	if !slices.Contains(dropped, corev1.Capability("ALL")) {
		result = append(result, violation{id: "WLK-CAPABILITIES-001", severity: models.Medium, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.capabilities.drop", currentValue: current, expectedValue: "ALL", message: fmt.Sprintf("Container %q does not drop all Linux capabilities by default.", container.name), recommendation: "Drop ALL capabilities and add back only the minimal capabilities the process requires.", documentationURL: podSecurityStandardsURL})
	}
	if container.securityContext != nil && container.securityContext.Capabilities != nil {
		var prohibited []string
		for _, capability := range container.securityContext.Capabilities.Add {
			name := strings.ToUpper(string(capability))
			if name != "NET_BIND_SERVICE" && baselineCapabilities[name] {
				prohibited = append(prohibited, name)
			}
		}
		if len(prohibited) > 0 {
			slices.Sort(prohibited)
			result = append(result, violation{id: "WLK-CAPABILITIES-RESTRICTED-001", severity: models.Medium, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.capabilities.add", currentValue: strings.Join(prohibited, ", "), expectedValue: "NET_BIND_SERVICE or none", message: fmt.Sprintf("Container %q adds capabilities prohibited by the restricted Pod Security Standard.", container.name), recommendation: "Remove added capabilities other than NET_BIND_SERVICE.", documentationURL: podSecurityStandardsURL})
		}
	}
	return result
}

func seccompViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) {
		return nil
	}
	profile := (*corev1.SeccompProfile)(nil)
	field := container.fieldPath + ".securityContext.seccompProfile"
	if target.podSpec.SecurityContext != nil {
		profile = target.podSpec.SecurityContext.SeccompProfile
		field = "spec.template.spec.securityContext.seccompProfile"
	}
	if container.securityContext != nil && container.securityContext.SeccompProfile != nil {
		profile = container.securityContext.SeccompProfile
		field = container.fieldPath + ".securityContext.seccompProfile"
	}
	if profile != nil && (profile.Type == corev1.SeccompProfileTypeRuntimeDefault || profile.Type == corev1.SeccompProfileTypeLocalhost) {
		return nil
	}
	current := "unset"
	if profile != nil {
		current = string(profile.Type)
	}
	return []violation{{id: "WLK-SECCOMP-001", severity: models.Medium, category: "security", container: container.name, fieldPath: field, currentValue: current, expectedValue: "RuntimeDefault or Localhost", message: fmt.Sprintf("Container %q does not use an allowed seccomp profile.", container.name), recommendation: "Set seccompProfile.type to RuntimeDefault unless a reviewed Localhost profile is required.", documentationURL: podSecurityStandardsURL}}
}

func readOnlyFilesystemViolations(target workloadTarget, container containerTarget) []violation {
	if isWindows(target.podSpec) || container.securityContext != nil && container.securityContext.ReadOnlyRootFilesystem != nil && *container.securityContext.ReadOnlyRootFilesystem {
		return nil
	}
	current := "unset"
	if container.securityContext != nil && container.securityContext.ReadOnlyRootFilesystem != nil {
		current = fmt.Sprintf("%t", *container.securityContext.ReadOnlyRootFilesystem)
	}
	return []violation{{id: "WLK-READONLYROOT-001", severity: models.Low, category: "security", container: container.name, fieldPath: container.fieldPath + ".securityContext.readOnlyRootFilesystem", currentValue: current, expectedValue: "true", message: fmt.Sprintf("Container %q can write to its root filesystem.", container.name), recommendation: "Set readOnlyRootFilesystem: true and mount explicit writable volumes for required paths.", documentationURL: podSecurityStandardsURL}}
}
