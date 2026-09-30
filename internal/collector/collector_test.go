package collector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestObservedAPIVersionsAreUniqueAndSorted(t *testing.T) {
	fields := []metav1.ManagedFieldsEntry{
		{APIVersion: "v1"},
		{APIVersion: "apps/v1"},
		{APIVersion: "v1"},
		{},
	}
	if got, want := observedAPIVersions(fields), []string{"apps/v1", "v1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observedAPIVersions() = %v, want %v", got, want)
	}
}

func TestHasVerb(t *testing.T) {
	if !hasVerb([]string{"get", "list"}, "list") {
		t.Fatal("hasVerb() did not find list")
	}
	if hasVerb([]string{"get"}, "list") {
		t.Fatal("hasVerb() found absent list")
	}
}

func TestComponentWorkloadResourcesExposeRawArguments(t *testing.T) {
	resources, err := componentWorkloadResources(
		[]appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "shop"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "checkout", Image: "example/checkout:1"}},
			}}},
		}},
		nil,
		[]appsv1.DaemonSet{{
			ObjectMeta: metav1.ObjectMeta{Name: "network-agent", Namespace: "kube-system"},
			Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "kube-proxy", Args: []string{"--feature-gates=SidecarContainers=true"}}},
			}}},
		}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Kind != "DaemonSet" || resources[0].Name != "network-agent" {
		t.Fatalf("component resources = %#v", resources)
	}
	if resources[0].Object == nil {
		t.Fatal("component resource has no raw object")
	}
}

func TestCollectOptionalEvidenceKeepsListFailuresNonFatal(t *testing.T) {
	client := fake.NewSimpleClientset(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "node-reader"}})
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods is forbidden")
	})

	pods, roles, warnings := collectOptionalEvidence(context.Background(), client)
	if len(pods) != 0 {
		t.Fatalf("pods = %#v, want none", pods)
	}
	if len(roles) != 1 || roles[0].Name != "node-reader" {
		t.Fatalf("roles = %#v, want independently collected ClusterRole", roles)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "list Pods") || !strings.Contains(warnings[0], "forbidden") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestCollectOptionalEvidenceKeepsClusterRoleFailuresNonFatal(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kube-apiserver", Namespace: "kube-system"}})
	client.PrependReactor("list", "clusterroles", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("clusterroles is forbidden")
	})

	pods, roles, warnings := collectOptionalEvidence(context.Background(), client)
	if len(pods) != 1 || pods[0].Name != "kube-apiserver" {
		t.Fatalf("pods = %#v, want independently collected Pod", pods)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %#v, want none", roles)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "list ClusterRoles") || !strings.Contains(warnings[0], "forbidden") {
		t.Fatalf("warnings = %#v", warnings)
	}
}
