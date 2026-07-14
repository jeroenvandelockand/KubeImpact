package knowledge

import (
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedRulesAreValid(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
	if got, want := SupportedVersions(), []string{"1.35", "1.36", "1.37"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedVersions() = %v, want %v", got, want)
	}
}

func TestLoadForUpgradeIsCumulative(t *testing.T) {
	rules, err := LoadForUpgrade("v1.34.8+k3s1", "v1.36.2")
	if err != nil {
		t.Fatalf("LoadForUpgrade() error = %v", err)
	}
	if len(rules) != 2 || rules[0].Version != "1.35" || rules[1].Version != "1.36" {
		t.Fatalf("LoadForUpgrade() versions = %#v", rules)
	}
}

func TestLoadForUpgradeRejectsGapsAndNonUpgrade(t *testing.T) {
	for _, test := range []struct{ current, target string }{
		{"1.35", "1.35"},
		{"1.36", "1.35"},
		{"1.33", "1.35"},
	} {
		if _, err := LoadForUpgrade(test.current, test.target); err == nil {
			t.Errorf("LoadForUpgrade(%q, %q) returned no error", test.current, test.target)
		}
	}
}

func TestResourceSelectorsThrough(t *testing.T) {
	selectors, err := ResourceSelectorsThrough("1.36")
	if err != nil {
		t.Fatalf("ResourceSelectorsThrough() error = %v", err)
	}
	if len(selectors) != 1 || selectors[0].GroupVersion != "storagemigration.k8s.io/v1alpha1" {
		t.Fatalf("ResourceSelectorsThrough() = %#v", selectors)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := decode([]byte("version: '1.35'\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("decode() error = %v", err)
	}
}

func TestValidateRejectsRemovedAPIRuleInWrongRelease(t *testing.T) {
	rules := &KubernetesRules{Version: "1.35", RemovedAPIs: []APIRule{{
		ID: "RULE", GroupVersion: "example.io/v1alpha1", Kind: "Widget", RemovedIn: "1.36",
		Message: "removed", Recommendation: "migrate", DocumentationURL: "https://example.com",
	}}}
	if err := validate(rules); err == nil || !strings.Contains(err.Error(), "removedIn") {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestNormalizeVersion(t *testing.T) {
	if got := NormalizeVersion(" v1.36.2+k3s1 "); got != "1.36" {
		t.Fatalf("NormalizeVersion() = %q", got)
	}
}
