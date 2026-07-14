package collector

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
