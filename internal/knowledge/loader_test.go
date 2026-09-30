package knowledge

import (
	"reflect"
	"strings"
	"testing"

	"kubeimpact/internal/models"
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

func TestResourceSelectorsThrough137SkipsSourceOnlyConfigurationAPIs(t *testing.T) {
	selectors, err := ResourceSelectorsThrough("1.37")
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[models.APIResourceSelector]bool{
		{GroupVersion: "scheduling.k8s.io/v1alpha2", Kind: "Workload"}:                     false,
		{GroupVersion: "scheduling.k8s.io/v1alpha2", Kind: "PodGroup"}:                     false,
		{GroupVersion: "certificates.k8s.io/v1beta1", Kind: "ClusterTrustBundle"}:          false,
		{GroupVersion: "certificates.k8s.io/v1beta1", Kind: "PodCertificateRequest"}:       false,
		{GroupVersion: "storagemigration.k8s.io/v1beta1", Kind: "StorageVersionMigration"}: false,
	}
	for _, selector := range selectors {
		if selector.GroupVersion == "kubeadm.k8s.io/v1beta3" {
			t.Fatalf("source-only kubeadm selector leaked into cluster inventory: %#v", selector)
		}
		if _, exists := wanted[selector]; exists {
			wanted[selector] = true
		}
	}
	for selector, found := range wanted {
		if !found {
			t.Errorf("missing selector %#v in %#v", selector, selectors)
		}
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

func TestValidateAllowsDeprecatedAPIWithoutKnownRemovalRelease(t *testing.T) {
	rules := &KubernetesRules{Version: "1.37", DeprecatedAPIs: []APIRule{{
		ID: "RULE", GroupVersion: "example.io/v1beta1", Kind: "Widget",
		Message: "deprecated", Recommendation: "migrate", DocumentationURL: "https://example.com",
	}}}
	if err := validate(rules); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestNormalizeVersion(t *testing.T) {
	if got := NormalizeVersion(" v1.36.2+k3s1 "); got != "1.36" {
		t.Fatalf("NormalizeVersion() = %q", got)
	}
}

func TestUpgradeFingerprintIsStableAndPathSpecific(t *testing.T) {
	first, err := UpgradeFingerprint("1.34", "1.35")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := UpgradeFingerprint("v1.34.9", "v1.35.1")
	if err != nil {
		t.Fatal(err)
	}
	longer, err := UpgradeFingerprint("1.34", "1.36")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || first != repeated || first == longer {
		t.Fatalf("fingerprints = %q, %q, %q", first, repeated, longer)
	}
}
