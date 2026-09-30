package collector

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"kubeimpact/internal/models"
)

type Snapshot struct {
	ClusterVersion string

	Deployments  []appsv1.Deployment
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
	Services     []corev1.Service
	Namespaces   []corev1.Namespace
	Events       []corev1.Event
	Pods         []corev1.Pod

	Resources             []models.KubernetesResource
	DeprecatedAPIRequests []models.DeprecatedAPIRequest
	Sources               map[string]string
	SourceResults         []models.SourceResult
	Warnings              []string
}

func Collect(ctx context.Context, selectors []models.APIResourceSelector) (*Snapshot, error) {
	cfg, err := kubernetesConfig()
	if err != nil {
		return nil, err
	}
	rest.AddUserAgent(cfg, "kubeimpact")

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("discover Kubernetes server version: %w", err)
	}

	deployments, err := clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Deployments: %w", err)
	}
	statefulSets, err := clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list StatefulSets: %w", err)
	}
	daemonSets, err := clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list DaemonSets: %w", err)
	}
	services, err := clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Services: %w", err)
	}
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Namespaces: %w", err)
	}
	eventItems := make([]corev1.Event, 0)
	eventWarnings := make([]string, 0)
	for _, reason := range []string{"SELinuxLabelConflict", "SELinuxChangePolicyConflict", "MultipleSELinuxLabels"} {
		events, eventErr := clientset.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("reason", reason).String(),
		})
		if eventErr != nil {
			eventWarnings = append(eventWarnings, fmt.Sprintf("SELinuxMount conflict Event evidence is unavailable for reason %s: %v", reason, eventErr))
			continue
		}
		eventItems = append(eventItems, events.Items...)
	}
	pods, clusterRoles, optionalWarnings := collectOptionalEvidence(ctx, clientset)

	resources, warnings := collectSelectedResources(ctx, cfg, clientset, selectors)
	warnings = append(warnings, eventWarnings...)
	warnings = append(warnings, optionalWarnings...)
	componentResources, conversionErr := componentWorkloadResources(deployments.Items, statefulSets.Items, daemonSets.Items, pods)
	if conversionErr != nil {
		return nil, conversionErr
	}
	resources = append(resources, componentResources...)
	for i := range clusterRoles {
		role := &clusterRoles[i]
		object, conversionErr := runtime.DefaultUnstructuredConverter.ToUnstructured(role)
		if conversionErr != nil {
			warnings = append(warnings, fmt.Sprintf("nodes/logs RBAC evidence is incomplete: convert ClusterRole %s: %v", role.Name, conversionErr))
			continue
		}
		resources = append(resources, models.KubernetesResource{
			Kind: "ClusterRole", Name: role.Name, ObservedAPIVersions: observedAPIVersions(role.ManagedFields),
			Source: "cluster", Object: object,
		})
	}
	deprecatedRequests, metricWarnings := collectDeprecatedAPIRequests(ctx, clientset)
	warnings = append(warnings, metricWarnings...)

	return &Snapshot{
		ClusterVersion:        version.GitVersion,
		Deployments:           deployments.Items,
		StatefulSets:          statefulSets.Items,
		DaemonSets:            daemonSets.Items,
		Services:              services.Items,
		Namespaces:            namespaces.Items,
		Events:                eventItems,
		Pods:                  pods,
		Resources:             resources,
		DeprecatedAPIRequests: deprecatedRequests,
		Sources:               map[string]string{},
		Warnings:              warnings,
	}, nil
}

// collectOptionalEvidence keeps additive upgrade checks from making the base
// scan unavailable when a deliberately narrow service account cannot list the
// extra resources. The warnings flow into Report.evidenceStatus as partial.
func collectOptionalEvidence(ctx context.Context, clientset kubernetes.Interface) ([]corev1.Pod, []rbacv1.ClusterRole, []string) {
	pods := make([]corev1.Pod, 0)
	clusterRoles := make([]rbacv1.ClusterRole, 0)
	warnings := make([]string, 0)

	podList, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Static Pod and component argument evidence is unavailable: list Pods: %v", err))
	} else {
		pods = podList.Items
	}

	roleList, err := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("nodes/logs RBAC evidence is unavailable: list ClusterRoles: %v", err))
	} else {
		clusterRoles = roleList.Items
	}

	return pods, clusterRoles, warnings
}

func componentWorkloadResources(
	deployments []appsv1.Deployment,
	statefulSets []appsv1.StatefulSet,
	daemonSets []appsv1.DaemonSet,
	pods []corev1.Pod,
) ([]models.KubernetesResource, error) {
	resources := make([]models.KubernetesResource, 0)
	appendObject := func(kind string, metadata metav1.Object, spec corev1.PodSpec, object any) error {
		if !isKubernetesComponentWorkload(metadata.GetName(), spec) {
			return nil
		}
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
		if err != nil {
			return fmt.Errorf("convert %s %s/%s: %w", kind, metadata.GetNamespace(), metadata.GetName(), err)
		}
		resources = append(resources, models.KubernetesResource{
			Kind: kind, Namespace: metadata.GetNamespace(), Name: metadata.GetName(), Namespaced: metadata.GetNamespace() != "",
			ObservedAPIVersions: observedAPIVersions(metadata.GetManagedFields()), Source: "cluster", Object: raw,
		})
		return nil
	}
	for i := range deployments {
		if err := appendObject("Deployment", &deployments[i], deployments[i].Spec.Template.Spec, &deployments[i]); err != nil {
			return nil, err
		}
	}
	for i := range statefulSets {
		if err := appendObject("StatefulSet", &statefulSets[i], statefulSets[i].Spec.Template.Spec, &statefulSets[i]); err != nil {
			return nil, err
		}
	}
	for i := range daemonSets {
		if err := appendObject("DaemonSet", &daemonSets[i], daemonSets[i].Spec.Template.Spec, &daemonSets[i]); err != nil {
			return nil, err
		}
	}
	for i := range pods {
		if err := appendObject("Pod", &pods[i], pods[i].Spec, &pods[i]); err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func isKubernetesComponentWorkload(name string, spec corev1.PodSpec) bool {
	componentName := func(value string) bool {
		value = strings.ToLower(value)
		for _, component := range []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "kube-proxy", "kubelet", "cloud-controller-manager"} {
			if strings.Contains(value, component) {
				return true
			}
		}
		return false
	}
	if componentName(name) {
		return true
	}
	for _, container := range append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...) {
		if componentName(container.Name) || componentName(container.Image) || len(container.Command) > 0 && componentName(container.Command[0]) {
			return true
		}
	}
	return false
}

func kubernetesConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster configuration or kubeconfig: %w", err)
	}
	return cfg, nil
}

func collectSelectedResources(
	ctx context.Context,
	cfg *rest.Config,
	clientset *kubernetes.Clientset,
	selectors []models.APIResourceSelector,
) ([]models.KubernetesResource, []string) {
	if len(selectors) == 0 {
		return []models.KubernetesResource{}, []string{}
	}

	warnings := []string{
		"Deprecated API detection uses metadata.managedFields and may miss clients or manifests that have not recorded their API version; verify source manifests and API request metrics before upgrading.",
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return []models.KubernetesResource{}, append(warnings, fmt.Sprintf("Deprecated API inventory is incomplete: create dynamic client: %v", err))
	}

	resources := make([]models.KubernetesResource, 0)
	seenSelectors := make(map[models.APIResourceSelector]struct{})
	for _, selector := range selectors {
		if _, seen := seenSelectors[selector]; seen {
			continue
		}
		seenSelectors[selector] = struct{}{}

		apiResourceList, discoveryErr := clientset.Discovery().ServerResourcesForGroupVersion(selector.GroupVersion)
		if discoveryErr != nil {
			if !apierrors.IsNotFound(discoveryErr) {
				warnings = append(warnings, fmt.Sprintf("Deprecated API inventory is incomplete for %s %s: discover resource: %v", selector.GroupVersion, selector.Kind, discoveryErr))
			}
			continue
		}

		for _, apiResource := range apiResourceList.APIResources {
			if apiResource.Kind != selector.Kind || !hasVerb(apiResource.Verbs, "list") {
				continue
			}

			groupVersion, parseErr := schema.ParseGroupVersion(selector.GroupVersion)
			if parseErr != nil {
				warnings = append(warnings, fmt.Sprintf("Deprecated API inventory is incomplete for %s %s: %v", selector.GroupVersion, selector.Kind, parseErr))
				break
			}
			gvr := groupVersion.WithResource(apiResource.Name)
			resourceClient := dynamicClient.Resource(gvr)

			var listErr error
			var items []models.KubernetesResource
			if apiResource.Namespaced {
				list, err := resourceClient.Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
				listErr = err
				if err == nil {
					items = resourcesFromList(list.Items, selector.Kind, selector.GroupVersion, true)
				}
			} else {
				list, err := resourceClient.List(ctx, metav1.ListOptions{})
				listErr = err
				if err == nil {
					items = resourcesFromList(list.Items, selector.Kind, selector.GroupVersion, false)
				}
			}
			if listErr != nil {
				warnings = append(warnings, fmt.Sprintf("Deprecated API inventory is incomplete for %s %s: list resources: %v", selector.GroupVersion, selector.Kind, listErr))
				break
			}
			resources = append(resources, items...)
			break
		}
	}

	sort.Strings(warnings)
	return resources, warnings
}

func resourcesFromList(items []unstructured.Unstructured, kind, listedAPIVersion string, namespaced bool) []models.KubernetesResource {
	resources := make([]models.KubernetesResource, 0, len(items))
	for i := range items {
		item := &items[i]
		resources = append(resources, models.KubernetesResource{
			Kind:                kind,
			Namespace:           item.GetNamespace(),
			Name:                item.GetName(),
			Namespaced:          namespaced,
			ObservedAPIVersions: observedAPIVersions(item.GetManagedFields()),
			ListedAPIVersion:    listedAPIVersion,
			Source:              "cluster",
			Object:              item.DeepCopy().Object,
		})
	}
	return resources
}

func observedAPIVersions(fields []metav1.ManagedFieldsEntry) []string {
	seen := make(map[string]struct{})
	versions := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.APIVersion == "" {
			continue
		}
		if _, exists := seen[field.APIVersion]; exists {
			continue
		}
		seen[field.APIVersion] = struct{}{}
		versions = append(versions, field.APIVersion)
	}
	sort.Strings(versions)
	return versions
}

func hasVerb(verbs []string, wanted string) bool {
	for _, verb := range verbs {
		if verb == wanted {
			return true
		}
	}
	return false
}
