package collector

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"

	"kubeimpact/internal/models"
)

const maxMetricsBytes = 25 << 20

var (
	deprecatedMetricPattern = regexp.MustCompile(`(?m)^apiserver_requested_deprecated_apis\{([^}]*)\}\s+([^\s]+)`)
	metricLabelPattern      = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:\\.|[^"])*)"`)
)

func collectDeprecatedAPIRequests(ctx context.Context, clientset *kubernetes.Clientset) ([]models.DeprecatedAPIRequest, []string) {
	stream, err := clientset.CoreV1().RESTClient().Get().AbsPath("/metrics").Stream(ctx)
	if err != nil {
		return []models.DeprecatedAPIRequest{}, []string{fmt.Sprintf("API request deprecation evidence is unavailable from /metrics: %v", err)}
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, maxMetricsBytes+1))
	if err != nil {
		return []models.DeprecatedAPIRequest{}, []string{fmt.Sprintf("API request deprecation metrics could not be read: %v", err)}
	}
	if len(raw) > maxMetricsBytes {
		return []models.DeprecatedAPIRequest{}, []string{fmt.Sprintf("API request deprecation metrics exceed the %d-byte safety limit", maxMetricsBytes)}
	}
	requests, err := parseDeprecatedAPIMetrics(raw)
	if err != nil {
		return []models.DeprecatedAPIRequest{}, []string{fmt.Sprintf("API request deprecation metrics could not be parsed: %v", err)}
	}
	return requests, []string{}
}

func parseDeprecatedAPIMetrics(data []byte) ([]models.DeprecatedAPIRequest, error) {
	seen := make(map[string]struct{})
	requests := make([]models.DeprecatedAPIRequest, 0)
	for _, match := range deprecatedMetricPattern.FindAllSubmatch(data, -1) {
		value, err := strconv.ParseFloat(string(match[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid metric value %q", match[2])
		}
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		labels := parseMetricLabels(string(match[1]))
		version := labels["version"]
		resource := labels["resource"]
		removed := labels["removed_release"]
		if version == "" || resource == "" || removed == "" {
			continue
		}
		groupVersion := version
		if group := labels["group"]; group != "" {
			groupVersion = group + "/" + version
		}
		request := models.DeprecatedAPIRequest{
			GroupVersion: groupVersion, Resource: resource, Subresource: labels["subresource"], RemovedRelease: removed,
		}
		key := strings.Join([]string{request.GroupVersion, request.Resource, request.Subresource, request.RemovedRelease}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, request)
	}
	sort.Slice(requests, func(i, j int) bool {
		return requests[i].GroupVersion+requests[i].Resource+requests[i].Subresource < requests[j].GroupVersion+requests[j].Resource+requests[j].Subresource
	})
	return requests, nil
}

func parseMetricLabels(raw string) map[string]string {
	labels := make(map[string]string)
	for _, match := range metricLabelPattern.FindAllStringSubmatch(raw, -1) {
		value, err := strconv.Unquote(`"` + match[2] + `"`)
		if err == nil {
			labels[match[1]] = value
		}
	}
	return labels
}
