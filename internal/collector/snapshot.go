package collector

import "fmt"

const (
	SourceAnnotation      = "internal.kubeimpact.io/source"
	SourceScopeAnnotation = "internal.kubeimpact.io/source-scope"
)

func ObjectSource(annotations map[string]string) string {
	if source := annotations[SourceAnnotation]; source != "" {
		return source
	}
	return "cluster"
}

func ObjectSourceScope(annotations map[string]string) string {
	if scope := annotations[SourceScopeAnnotation]; scope != "" {
		return scope
	}
	return ObjectSource(annotations)
}

func ResourceKey(kind, namespace, name string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", kind, namespace, name)
}

func (s *Snapshot) SourceFor(kind, namespace, name string) string {
	if s == nil || s.Sources == nil {
		return "cluster"
	}
	if source := s.Sources[ResourceKey(kind, namespace, name)]; source != "" {
		return source
	}
	return "cluster"
}

func (s *Snapshot) NamespaceLabels(namespace, sourceScope string) map[string]string {
	if s == nil {
		return nil
	}
	for i := range s.Namespaces {
		if s.Namespaces[i].Name == namespace && ObjectSourceScope(s.Namespaces[i].Annotations) == sourceScope {
			return s.Namespaces[i].Labels
		}
	}
	return nil
}

func Merge(destination, source *Snapshot) {
	if destination == nil || source == nil {
		return
	}
	destination.Deployments = append(destination.Deployments, source.Deployments...)
	destination.StatefulSets = append(destination.StatefulSets, source.StatefulSets...)
	destination.DaemonSets = append(destination.DaemonSets, source.DaemonSets...)
	destination.Services = append(destination.Services, source.Services...)
	destination.Namespaces = append(destination.Namespaces, source.Namespaces...)
	destination.Resources = append(destination.Resources, source.Resources...)
	destination.DeprecatedAPIRequests = append(destination.DeprecatedAPIRequests, source.DeprecatedAPIRequests...)
	destination.SourceResults = append(destination.SourceResults, source.SourceResults...)
	destination.Warnings = append(destination.Warnings, source.Warnings...)
	if destination.Sources == nil {
		destination.Sources = map[string]string{}
	}
	for key, value := range source.Sources {
		destination.Sources[key] = value
	}
}
