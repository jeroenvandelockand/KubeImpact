package collector

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

	Resources []models.KubernetesResource
	Warnings  []string
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

	resources, warnings := collectSelectedResources(ctx, cfg, clientset, selectors)

	return &Snapshot{
		ClusterVersion: version.GitVersion,
		Deployments:    deployments.Items,
		StatefulSets:   statefulSets.Items,
		DaemonSets:     daemonSets.Items,
		Services:       services.Items,
		Resources:      resources,
		Warnings:       warnings,
	}, nil
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
					items = resourcesFromList(list.Items, selector.Kind, true)
				}
			} else {
				list, err := resourceClient.List(ctx, metav1.ListOptions{})
				listErr = err
				if err == nil {
					items = resourcesFromList(list.Items, selector.Kind, false)
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

func resourcesFromList(items []unstructured.Unstructured, kind string, namespaced bool) []models.KubernetesResource {
	resources := make([]models.KubernetesResource, 0, len(items))
	for i := range items {
		item := &items[i]
		resources = append(resources, models.KubernetesResource{
			Kind:                kind,
			Namespace:           item.GetNamespace(),
			Name:                item.GetName(),
			Namespaced:          namespaced,
			ObservedAPIVersions: observedAPIVersions(item.GetManagedFields()),
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
