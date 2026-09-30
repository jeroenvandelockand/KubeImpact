package upgrade

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/knowledge"
	"kubeimpact/internal/models"
)

var removedFeatureGates137 = map[string]struct{}{
	"GangScheduling": {}, "WorkloadAwarePreemption": {}, "AnyVolumeDataSource": {},
	"RelaxedDNSSearchValidation": {}, "RetryGenerateName": {}, "BtreeWatchCache": {},
	"OrderedNamespaceDeletion": {}, "StreamingCollectionEncodingToJSON": {},
	"StreamingCollectionEncodingToProtobuf": {}, "APIServerTracing": {},
	"ResilientWatchCacheInitialization": {}, "ConsistentListFromCache": {},
	"PreventStaticPodAPIReferences": {}, "SidecarContainers": {},
	"NodeLocalCRISocket": {}, "PublicKeysECDSA": {},
}

var lockedFeatureGates137 = map[string]bool{
	"DeclarativeValidationTakeover": false,
	"DRAPrioritizedList":            true,
	"HostnameOverride":              true,
}

var removedComponentFlags137 = []string{
	"application-metrics-count-limit", "boot-id-file", "container-hints", "containerd-namespace", "containerd",
	"enable-load-reader", "event-storage-age-limit", "event-storage-event-limit", "global-housekeeping-interval",
	"log-cadvisor-usage", "machine-id-file", "storage-driver-user", "storage-driver-password", "storage-driver-host",
	"storage-driver-db", "storage-driver-table", "storage-driver-secure", "storage-driver-buffer-duration",
	"concurrent-service-syncs",
}

func evaluateKubeletEventRecordQPS(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	return evaluateConfigurationField(check, snapshot, "KubeletConfiguration", []string{"eventRecordQPS"}, func(value any, found bool) bool {
		return found && isZero(value)
	}, "explicitly configured as 0", "an explicit non-zero rate limit")
}

func evaluateKubeletCgroupV1Override(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	return evaluateConfigurationField(check, snapshot, "KubeletConfiguration", []string{"failCgroupV1"}, func(value any, found bool) bool {
		return found && isFalse(value)
	}, "false", "cgroup v2 with the compatibility override removed")
}

func evaluateCloudNodeMonitorPeriod(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	return evaluateConfigurationField(check, snapshot, "CloudControllerManagerConfiguration", []string{"kubeCloudShared", "nodeMonitorPeriod"}, func(_ any, found bool) bool {
		return found
	}, "kubeCloudShared.nodeMonitorPeriod", "nodeLifecycleController.nodeMonitorPeriod")
}

func evaluateRemovedFeatureGates(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for _, resource := range snapshot.Resources {
		if resource.Object == nil {
			continue
		}
		settings := findFeatureGateSettings(resource)
		gates := make([]string, 0, len(settings))
		for gate := range settings {
			if _, removed := removedFeatureGates137[gate]; removed {
				gates = append(gates, gate)
			}
		}
		sort.Strings(gates)
		for _, gate := range gates {
			setting := settings[gate]
			impacts = append(impacts, resourceImpact(check, resource, setting.path, gate+"="+setting.value, "feature gate setting removed"))
		}
	}
	return impacts
}

func evaluateLockedFeatureGates(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for _, resource := range snapshot.Resources {
		if resource.Object == nil {
			continue
		}
		settings := findFeatureGateSettings(resource)
		gates := make([]string, 0, len(settings))
		for gate, setting := range settings {
			lockedDefault, locked := lockedFeatureGates137[gate]
			configured, err := strconv.ParseBool(strings.TrimSpace(setting.value))
			if locked && err == nil && configured != lockedDefault {
				gates = append(gates, gate)
			}
		}
		sort.Strings(gates)
		for _, gate := range gates {
			setting := settings[gate]
			lockedDefault := lockedFeatureGates137[gate]
			impacts = append(impacts, resourceImpact(
				check,
				resource,
				setting.path,
				gate+"="+setting.value,
				gate+"="+strconv.FormatBool(lockedDefault),
			))
		}
	}
	return impacts
}

func evaluateRemovedComponentFlags(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for _, resource := range snapshot.Resources {
		if resource.Object == nil {
			continue
		}
		settings := findRemovedFlagSettings(resource)
		flags := make([]string, 0, len(settings))
		for flag := range settings {
			flags = append(flags, flag)
		}
		sort.Strings(flags)
		for _, flag := range flags {
			impacts = append(impacts, resourceImpact(check, resource, settings[flag], "--"+flag, "flag removed"))
		}
	}
	return impacts
}

type locatedSetting struct{ path, value string }

func findFeatureGateSettings(resource models.KubernetesResource) map[string]locatedSetting {
	settings := make(map[string]locatedSetting)
	apiVersion := objectAPIVersion(resource.Object)
	switch resource.Kind {
	case "KubeletConfiguration":
		if strings.HasPrefix(apiVersion, "kubelet.config.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "")
		}
	case "KubeProxyConfiguration":
		if strings.HasPrefix(apiVersion, "kubeproxy.config.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "")
		}
	case "KubeSchedulerConfiguration":
		if strings.HasPrefix(apiVersion, "kubescheduler.config.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "")
		}
	case "KubeControllerManagerConfiguration":
		if strings.HasPrefix(apiVersion, "kubecontrollermanager.config.k8s.io/") || strings.HasPrefix(apiVersion, "controllermanager.config.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "")
		}
	case "CloudControllerManagerConfiguration":
		if strings.HasPrefix(apiVersion, "cloudcontrollermanager.config.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "")
		}
	case "ClusterConfiguration":
		if strings.HasPrefix(apiVersion, "kubeadm.k8s.io/") {
			parseFeatureGateMap(settings, resource.Object, "featureGates", "kubeadm")
			parseFeatureGatesFromExtraArgs(settings, resource.Object, []string{"apiServer", "extraArgs"})
			parseFeatureGatesFromExtraArgs(settings, resource.Object, []string{"controllerManager", "extraArgs"})
			parseFeatureGatesFromExtraArgs(settings, resource.Object, []string{"scheduler", "extraArgs"})
		}
	case "InitConfiguration", "JoinConfiguration":
		if strings.HasPrefix(apiVersion, "kubeadm.k8s.io/") {
			parseFeatureGatesFromExtraArgs(settings, resource.Object, []string{"nodeRegistration", "kubeletExtraArgs"})
		}
	default:
		for _, arguments := range componentArgumentValues(resource) {
			parseFeatureGateArguments(settings, arguments.path, arguments.value)
		}
	}
	return settings
}

func parseFeatureGateMap(settings map[string]locatedSetting, object map[string]any, key, component string) {
	value, found := nestedField(object, key)
	if !found {
		return
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return
	}
	for gate, enabled := range mapping {
		if component == "kubeadm" && gate != "NodeLocalCRISocket" && gate != "PublicKeysECDSA" {
			continue
		}
		settings[gate] = locatedSetting{path: key + "[" + gate + "]", value: fmt.Sprint(enabled)}
	}
}

func parseFeatureGatesFromExtraArgs(settings map[string]locatedSetting, object map[string]any, path []string) {
	value, found := nestedField(object, path...)
	if !found {
		return
	}
	fieldPath := strings.Join(path, ".")
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if normalizeFlagName(key) == "feature-gates" {
				parseFeatureGateList(settings, fieldPath+"[feature-gates]", fmt.Sprint(item))
			}
		}
	case []any:
		for index, item := range typed {
			argument, ok := item.(map[string]any)
			if !ok || normalizeFlagName(fmt.Sprint(argument["name"])) != "feature-gates" {
				continue
			}
			parseFeatureGateList(settings, fmt.Sprintf("%s[%d].value", fieldPath, index), fmt.Sprint(argument["value"]))
		}
	}
}

func parseFeatureGateArguments(settings map[string]locatedSetting, path string, value any) {
	arguments, ok := value.([]any)
	if !ok {
		return
	}
	for index := 0; index < len(arguments); index++ {
		argument, ok := arguments[index].(string)
		if !ok {
			continue
		}
		if list, found := strings.CutPrefix(argument, "--feature-gates="); found {
			parseFeatureGateList(settings, fmt.Sprintf("%s[%d]", path, index), list)
			continue
		}
		if normalizeFlagName(argument) == "feature-gates" && index+1 < len(arguments) {
			if gateList, ok := arguments[index+1].(string); ok {
				parseFeatureGateList(settings, fmt.Sprintf("%s[%d]", path, index+1), gateList)
			}
		}
	}
}

func parseFeatureGateList(settings map[string]locatedSetting, path, value string) {
	for _, entry := range strings.Split(value, ",") {
		gate, enabled, found := strings.Cut(strings.TrimSpace(entry), "=")
		if !found {
			enabled = "configured"
		}
		if gate != "" {
			settings[gate] = locatedSetting{path: path, value: enabled}
		}
	}
}

func findRemovedFlagSettings(resource models.KubernetesResource) map[string]string {
	settings := make(map[string]string)
	apiVersion := objectAPIVersion(resource.Object)
	if strings.HasPrefix(apiVersion, "kubeadm.k8s.io/") {
		switch resource.Kind {
		case "InitConfiguration", "JoinConfiguration":
			parseRemovedFlagsFromExtraArgs(settings, resource.Object, []string{"nodeRegistration", "kubeletExtraArgs"}, "kubelet")
		case "ClusterConfiguration":
			parseRemovedFlagsFromExtraArgs(settings, resource.Object, []string{"controllerManager", "extraArgs"}, "kube-controller-manager")
		}
	}
	for _, arguments := range componentArgumentValues(resource) {
		parseRemovedFlagArguments(settings, arguments.path, arguments.value, arguments.component)
	}
	return settings
}

func parseRemovedFlagsFromExtraArgs(settings map[string]string, object map[string]any, path []string, component string) {
	value, found := nestedField(object, path...)
	if !found {
		return
	}
	fieldPath := strings.Join(path, ".")
	switch typed := value.(type) {
	case map[string]any:
		for name := range typed {
			flag := normalizeFlagName(name)
			if removedFlagBelongsTo(flag, component) {
				settings[flag] = fieldPath + "[" + flag + "]"
			}
		}
	case []any:
		for index, item := range typed {
			argument, ok := item.(map[string]any)
			if !ok {
				continue
			}
			flag := normalizeFlagName(fmt.Sprint(argument["name"]))
			if removedFlagBelongsTo(flag, component) {
				settings[flag] = fmt.Sprintf("%s[%d].name", fieldPath, index)
			}
		}
	}
}

func parseRemovedFlagArguments(settings map[string]string, path string, value any, component string) {
	arguments, ok := value.([]any)
	if !ok {
		return
	}
	for index, item := range arguments {
		argument, ok := item.(string)
		if !ok || !strings.HasPrefix(argument, "--") {
			continue
		}
		name := strings.TrimPrefix(argument, "--")
		if separator := strings.IndexByte(name, '='); separator >= 0 {
			name = name[:separator]
		}
		if removedFlagBelongsTo(name, component) {
			settings[name] = fmt.Sprintf("%s[%d]", path, index)
		}
	}
}

func removedFlagBelongsTo(flag, component string) bool {
	if flag == "concurrent-service-syncs" {
		return component == "kube-controller-manager"
	}
	if component != "kubelet" {
		return false
	}
	for _, removed := range removedComponentFlags137 {
		if flag == removed && flag != "concurrent-service-syncs" {
			return true
		}
	}
	return false
}

type componentArguments struct {
	component string
	path      string
	value     any
}

func componentArgumentValues(resource models.KubernetesResource) []componentArguments {
	apiVersion := objectAPIVersion(resource.Object)
	if apiVersion == "" && resource.Source != "" && resource.Source != "cluster" {
		return nil
	}
	var podSpecPath []string
	switch resource.Kind {
	case "Pod":
		if apiVersion != "" && apiVersion != "v1" {
			return nil
		}
		podSpecPath = []string{"spec"}
	case "Deployment", "StatefulSet", "DaemonSet":
		if apiVersion != "" && !strings.HasPrefix(apiVersion, "apps/") {
			return nil
		}
		podSpecPath = []string{"spec", "template", "spec"}
	case "ReplicationController":
		if apiVersion != "" && apiVersion != "v1" {
			return nil
		}
		podSpecPath = []string{"spec", "template", "spec"}
	default:
		return nil
	}

	result := make([]componentArguments, 0)
	for _, containerField := range []string{"initContainers", "containers"} {
		path := append(append([]string{}, podSpecPath...), containerField)
		value, found := nestedField(resource.Object, path...)
		if !found {
			continue
		}
		containers, ok := value.([]any)
		if !ok {
			continue
		}
		for index, item := range containers {
			container, ok := item.(map[string]any)
			if !ok {
				continue
			}
			component := componentFromContainer(container)
			if component == "" {
				continue
			}
			for _, field := range []string{"command", "args"} {
				arguments, exists := container[field]
				if !exists {
					continue
				}
				result = append(result, componentArguments{
					component: component,
					path:      fmt.Sprintf("%s.%s[%d].%s", strings.Join(podSpecPath, "."), containerField, index, field),
					value:     arguments,
				})
			}
		}
	}
	return result
}

func componentFromContainer(container map[string]any) string {
	values := []string{fmt.Sprint(container["name"]), fmt.Sprint(container["image"])}
	if command, ok := container["command"].([]any); ok && len(command) > 0 {
		values = append(values, fmt.Sprint(command[0]))
	}
	for _, value := range values {
		value = strings.ToLower(value)
		for _, component := range []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "kube-proxy", "kubelet", "cloud-controller-manager"} {
			if strings.Contains(value, component) {
				return component
			}
		}
	}
	return ""
}

func objectAPIVersion(object map[string]any) string {
	value, found := object["apiVersion"]
	if !found || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func normalizeFlagName(value string) string {
	return strings.TrimLeft(strings.TrimSpace(value), "-")
}

func evaluateKubeProxyIPVS(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	return evaluateConfigurationField(check, snapshot, "KubeProxyConfiguration", []string{"mode"}, func(value any, found bool) bool {
		return found && strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "ipvs")
	}, "ipvs", "nftables or iptables")
}

func evaluateKubeProxyModeUnset(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	return evaluateConfigurationField(check, snapshot, "KubeProxyConfiguration", []string{"mode"}, func(value any, found bool) bool {
		return !found || strings.TrimSpace(fmt.Sprint(value)) == ""
	}, "unset", "an explicit nftables or iptables mode")
}

func evaluateNodeLogsRBAC(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for _, resource := range snapshot.Resources {
		if resource.Kind != "ClusterRole" || resource.Object == nil {
			continue
		}
		rules, found := nestedField(resource.Object, "rules")
		if !found {
			continue
		}
		list, ok := rules.([]any)
		if !ok {
			continue
		}
		for index, item := range list {
			rule, ok := item.(map[string]any)
			if !ok || !stringListContains(rule["resources"], "nodes/logs") || !stringListIntersects(rule["verbs"], "get", "list", "watch", "*") {
				continue
			}
			fieldPath := fmt.Sprintf("rules[%d].resources", index)
			impacts = append(impacts, resourceImpact(check, resource, fieldPath, "nodes/logs", "access restricted to explicitly trusted subjects"))
		}
	}
	return impacts
}

func evaluateStaticPodAPIReferences(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for i := range snapshot.Pods {
		pod := &snapshot.Pods[i]
		if !isStaticPod(pod.Annotations) {
			continue
		}
		fieldPath, reference, found := podAPIReference(&pod.Spec)
		if !found {
			continue
		}
		source := collector.ObjectSource(pod.Annotations)
		impacts = append(impacts, models.UpgradeImpact{
			Rule:        check.ID,
			Fingerprint: models.NewFingerprint("upgrade", check.ID, source, pod.Namespace, "Pod", pod.Name, fieldPath),
			Severity:    models.Severity(check.Severity), Category: "api",
			Namespace: pod.Namespace, Kind: "Pod", Name: pod.Name, FieldPath: fieldPath, Source: source,
			CurrentValue: reference, ExpectedValue: "no Kubernetes API object references",
			Message: check.Message, Recommendation: check.Recommendation, DocumentationURL: check.DocumentationURL,
		})
	}
	return impacts
}

func isStaticPod(annotations map[string]string) bool {
	if annotations["kubernetes.io/config.mirror"] != "" {
		return true
	}
	switch annotations["kubernetes.io/config.source"] {
	case "file", "http":
		return true
	default:
		return false
	}
}

func podAPIReference(spec *corev1.PodSpec) (string, string, bool) {
	if spec.ServiceAccountName != "" {
		return "spec.serviceAccountName", "ServiceAccount " + spec.ServiceAccountName, true
	}
	if len(spec.ImagePullSecrets) > 0 {
		return "spec.imagePullSecrets", "Secret " + spec.ImagePullSecrets[0].Name, true
	}
	if len(spec.ResourceClaims) > 0 {
		return "spec.resourceClaims", "ResourceClaim", true
	}
	containerGroups := [][]corev1.Container{spec.InitContainers, spec.Containers}
	for _, containers := range containerGroups {
		for _, container := range containers {
			if fieldPath, reference, found := containerAPIReference(container); found {
				return fieldPath, reference, true
			}
		}
	}
	for _, container := range spec.EphemeralContainers {
		if fieldPath, reference, found := containerAPIReference(corev1.Container(container.EphemeralContainerCommon)); found {
			return fieldPath, reference, true
		}
	}
	for index, volume := range spec.Volumes {
		fieldPath := fmt.Sprintf("spec.volumes[%d]", index)
		switch {
		case volume.RBD != nil && volume.RBD.SecretRef != nil:
			return fieldPath + ".rbd.secretRef", "Secret " + volume.RBD.SecretRef.Name, true
		case volume.CephFS != nil && volume.CephFS.SecretRef != nil:
			return fieldPath + ".cephfs.secretRef", "Secret " + volume.CephFS.SecretRef.Name, true
		case volume.Cinder != nil && volume.Cinder.SecretRef != nil:
			return fieldPath + ".cinder.secretRef", "Secret " + volume.Cinder.SecretRef.Name, true
		case volume.FlexVolume != nil && volume.FlexVolume.SecretRef != nil:
			return fieldPath + ".flexVolume.secretRef", "Secret " + volume.FlexVolume.SecretRef.Name, true
		case volume.ISCSI != nil && volume.ISCSI.SecretRef != nil:
			return fieldPath + ".iscsi.secretRef", "Secret " + volume.ISCSI.SecretRef.Name, true
		case volume.ScaleIO != nil && volume.ScaleIO.SecretRef != nil:
			return fieldPath + ".scaleIO.secretRef", "Secret " + volume.ScaleIO.SecretRef.Name, true
		case volume.StorageOS != nil && volume.StorageOS.SecretRef != nil:
			return fieldPath + ".storageos.secretRef", "Secret " + volume.StorageOS.SecretRef.Name, true
		case volume.ConfigMap != nil:
			return fieldPath + ".configMap", "ConfigMap " + volume.ConfigMap.Name, true
		case volume.Secret != nil:
			return fieldPath + ".secret", "Secret " + volume.Secret.SecretName, true
		case volume.PersistentVolumeClaim != nil:
			return fieldPath + ".persistentVolumeClaim", "PersistentVolumeClaim " + volume.PersistentVolumeClaim.ClaimName, true
		case volume.Ephemeral != nil:
			return fieldPath + ".ephemeral", "PersistentVolumeClaim via ephemeral volume", true
		case volume.CSI != nil:
			return fieldPath + ".csi", "CSIDriver " + volume.CSI.Driver, true
		case volume.Glusterfs != nil:
			return fieldPath + ".glusterfs", "Endpoints " + volume.Glusterfs.EndpointsName, true
		case volume.AzureFile != nil:
			return fieldPath + ".azureFile", "Secret " + volume.AzureFile.SecretName, true
		case volume.Projected != nil:
			for sourceIndex, source := range volume.Projected.Sources {
				projectedPath := fmt.Sprintf("%s.projected.sources[%d]", fieldPath, sourceIndex)
				switch {
				case source.ConfigMap != nil:
					return projectedPath + ".configMap", "ConfigMap " + source.ConfigMap.Name, true
				case source.Secret != nil:
					return projectedPath + ".secret", "Secret " + source.Secret.Name, true
				case source.ServiceAccountToken != nil:
					return projectedPath + ".serviceAccountToken", "ServiceAccount token", true
				case source.ClusterTrustBundle != nil:
					return projectedPath + ".clusterTrustBundle", "ClusterTrustBundle", true
				case source.PodCertificate != nil:
					return projectedPath + ".podCertificate", "PodCertificate", true
				}
			}
		}
	}
	return "", "", false
}

func containerAPIReference(container corev1.Container) (string, string, bool) {
	for index, source := range container.EnvFrom {
		if source.ConfigMapRef != nil {
			return fmt.Sprintf("spec.containers[name=%s].envFrom[%d].configMapRef", container.Name, index), "ConfigMap " + source.ConfigMapRef.Name, true
		}
		if source.SecretRef != nil {
			return fmt.Sprintf("spec.containers[name=%s].envFrom[%d].secretRef", container.Name, index), "Secret " + source.SecretRef.Name, true
		}
	}
	for index, variable := range container.Env {
		if variable.ValueFrom == nil {
			continue
		}
		if variable.ValueFrom.ConfigMapKeyRef != nil {
			return fmt.Sprintf("spec.containers[name=%s].env[%d].valueFrom.configMapKeyRef", container.Name, index), "ConfigMap " + variable.ValueFrom.ConfigMapKeyRef.Name, true
		}
		if variable.ValueFrom.SecretKeyRef != nil {
			return fmt.Sprintf("spec.containers[name=%s].env[%d].valueFrom.secretKeyRef", container.Name, index), "Secret " + variable.ValueFrom.SecretKeyRef.Name, true
		}
	}
	return "", "", false
}

func evaluateConfigurationField(
	check knowledge.ResourceCheck,
	snapshot *collector.Snapshot,
	kind string,
	path []string,
	matches func(any, bool) bool,
	currentValue string,
	expectedValue string,
) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	for _, resource := range snapshot.Resources {
		if resource.Kind != kind || resource.Object == nil {
			continue
		}
		value, found := nestedField(resource.Object, path...)
		if !matches(value, found) {
			continue
		}
		fieldPath := strings.Join(path, ".")
		impacts = append(impacts, resourceImpact(check, resource, fieldPath, currentValue, expectedValue))
	}
	return impacts
}

func evaluateSELinuxVolumeConflicts(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	seen := make(map[string]struct{})
	for i := range snapshot.Events {
		event := &snapshot.Events[i]
		if event.Source.Component != "selinux_warning" {
			continue
		}
		switch event.Reason {
		case "SELinuxLabelConflict", "SELinuxChangePolicyConflict", "MultipleSELinuxLabels":
		default:
			continue
		}
		kind := event.InvolvedObject.Kind
		if kind == "" {
			kind = "Pod"
		}
		name := event.InvolvedObject.Name
		if name == "" {
			name = event.Name
		}
		namespace := event.InvolvedObject.Namespace
		if namespace == "" {
			namespace = event.Namespace
		}
		source := collector.ObjectSource(event.Annotations)
		fieldPath := "event.reason"
		fingerprint := models.NewFingerprint("upgrade", check.ID, source, namespace, kind, name, event.Reason)
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		impacts = append(impacts, models.UpgradeImpact{
			Rule: check.ID, Fingerprint: fingerprint, Severity: models.Severity(check.Severity), Category: "storage",
			Namespace: namespace, Kind: kind, Name: name, FieldPath: fieldPath, Source: source,
			CurrentValue: event.Reason + ": " + event.Message, ExpectedValue: "no SELinux volume conflicts",
			Message: check.Message, Recommendation: check.Recommendation, DocumentationURL: check.DocumentationURL,
		})
	}
	return impacts
}

func evaluateKubeDNS(check knowledge.ResourceCheck, snapshot *collector.Snapshot) []models.UpgradeImpact {
	impacts := make([]models.UpgradeImpact, 0)
	seen := make(map[string]struct{})
	add := func(kind string, object metav1.Object, source, containerName, image string) {
		fieldPath := fmt.Sprintf("spec.template.spec.containers[name=%s].image", containerName)
		if kind == "Pod" {
			fieldPath = fmt.Sprintf("spec.containers[name=%s].image", containerName)
		}
		fingerprint := models.NewFingerprint("upgrade", check.ID, source, object.GetNamespace(), kind, object.GetName(), fieldPath)
		if _, exists := seen[fingerprint]; exists {
			return
		}
		seen[fingerprint] = struct{}{}
		impacts = append(impacts, models.UpgradeImpact{
			Rule: check.ID, Fingerprint: fingerprint, Severity: models.Severity(check.Severity), Category: "networking",
			Namespace: object.GetNamespace(), Kind: kind, Name: object.GetName(), FieldPath: fieldPath, Source: source,
			Container: containerName, CurrentValue: image, ExpectedValue: "a CoreDNS container image",
			Message: check.Message, Recommendation: check.Recommendation, DocumentationURL: check.DocumentationURL,
		})
	}
	for i := range snapshot.Deployments {
		for _, container := range snapshot.Deployments[i].Spec.Template.Spec.Containers {
			if legacyKubeDNSContainer(container.Name, container.Image) {
				add("Deployment", &snapshot.Deployments[i], collector.ObjectSource(snapshot.Deployments[i].Annotations), container.Name, container.Image)
			}
		}
	}
	for i := range snapshot.StatefulSets {
		for _, container := range snapshot.StatefulSets[i].Spec.Template.Spec.Containers {
			if legacyKubeDNSContainer(container.Name, container.Image) {
				add("StatefulSet", &snapshot.StatefulSets[i], collector.ObjectSource(snapshot.StatefulSets[i].Annotations), container.Name, container.Image)
			}
		}
	}
	for i := range snapshot.DaemonSets {
		for _, container := range snapshot.DaemonSets[i].Spec.Template.Spec.Containers {
			if legacyKubeDNSContainer(container.Name, container.Image) {
				add("DaemonSet", &snapshot.DaemonSets[i], collector.ObjectSource(snapshot.DaemonSets[i].Annotations), container.Name, container.Image)
			}
		}
	}
	for i := range snapshot.Pods {
		for _, container := range snapshot.Pods[i].Spec.Containers {
			if legacyKubeDNSContainer(container.Name, container.Image) {
				add("Pod", &snapshot.Pods[i], collector.ObjectSource(snapshot.Pods[i].Annotations), container.Name, container.Image)
			}
		}
	}
	for i := range snapshot.Resources {
		resource := &snapshot.Resources[i]
		if resource.Kind != "ReplicationController" {
			continue
		}
		object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
			Name: resource.Name, Namespace: resource.Namespace,
		}}
		for _, container := range rawContainers(resource.Object, "spec", "template", "spec", "containers") {
			if legacyKubeDNSContainer(container.name, container.image) {
				add(resource.Kind, object, resource.Source, container.name, container.image)
			}
		}
	}
	return impacts
}

func legacyKubeDNSContainer(name, image string) bool {
	if name == "kubedns" {
		return true
	}
	basename := image
	if slash := strings.LastIndex(basename, "/"); slash >= 0 {
		basename = basename[slash+1:]
	}
	basename = strings.SplitN(basename, "@", 2)[0]
	basename = strings.SplitN(basename, ":", 2)[0]
	return strings.Contains(basename, "k8s-dns-kube-dns")
}

type rawContainer struct{ name, image string }

func rawContainers(object map[string]any, path ...string) []rawContainer {
	value, found := nestedField(object, path...)
	if !found {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	containers := make([]rawContainer, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		containers = append(containers, rawContainer{name: fmt.Sprint(mapping["name"]), image: fmt.Sprint(mapping["image"])})
	}
	return containers
}

func resourceImpact(check knowledge.ResourceCheck, resource models.KubernetesResource, fieldPath, current, expected string) models.UpgradeImpact {
	return models.UpgradeImpact{
		Rule:        check.ID,
		Fingerprint: models.NewFingerprint("upgrade", check.ID, resource.Source, resource.Namespace, resource.Kind, resource.Name, fieldPath),
		Severity:    models.Severity(check.Severity), Category: "configuration",
		Namespace: resource.Namespace, Kind: resource.Kind, Name: resource.Name, FieldPath: fieldPath, Source: resource.Source,
		CurrentValue: current, ExpectedValue: expected,
		Message: check.Message, Recommendation: check.Recommendation, DocumentationURL: check.DocumentationURL,
	}
}

func nestedField(object map[string]any, path ...string) (any, bool) {
	var current any = object
	for _, component := range path {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[component]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func stringMapAt(object map[string]any, path ...string) map[string]string {
	value, found := nestedField(object, path...)
	if !found {
		return map[string]string{}
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	result := make(map[string]string, len(mapping))
	for key, item := range mapping {
		if text, ok := item.(string); ok {
			result[key] = text
		}
	}
	return result
}

func stringListContains(value any, wanted string) bool {
	return stringListIntersects(value, wanted)
}

func stringListIntersects(value any, wanted ...string) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	accepted := make(map[string]struct{}, len(wanted))
	for _, item := range wanted {
		accepted[item] = struct{}{}
	}
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if _, exists := accepted[text]; exists {
			return true
		}
	}
	return false
}

func isZero(value any) bool {
	switch typed := value.(type) {
	case int:
		return typed == 0
	case int32:
		return typed == 0
	case int64:
		return typed == 0
	case float32:
		return typed == 0
	case float64:
		return typed == 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return err == nil && parsed == 0
	default:
		return false
	}
}

func isFalse(value any) bool {
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && !parsed
	default:
		return false
	}
}
