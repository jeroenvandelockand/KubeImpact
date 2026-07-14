package policy

import (
	"os"
	"path/filepath"
	"testing"

	"kubeimpact/internal/models"
)

func TestConfigExclusionsAndSeverityOverrides(t *testing.T) {
	config := Config{
		Profile: ProfileRestricted,
		Exclusions: Exclusions{
			Namespaces: []string{"kube-*"}, WorkloadLabels: map[string]string{"skip": "true"}, NamespaceLabels: map[string]string{"team": "platform"},
		},
		SeverityOverrides: map[string]models.Severity{"RULE": models.Critical},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !config.Excludes("kube-system", nil, nil) || !config.Excludes("default", map[string]string{"skip": "true"}, nil) || !config.Excludes("apps", nil, map[string]string{"team": "platform"}) {
		t.Fatal("expected exclusions to match")
	}
	if config.Excludes("apps", map[string]string{"skip": "false"}, nil) {
		t.Fatal("unexpected exclusion")
	}
	if config.Severity("RULE", models.Low) != models.Critical || config.Severity("OTHER", models.Low) != models.Low {
		t.Fatal("severity override not applied")
	}
}

func TestLoadIsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte("profile: baseline\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted unknown field")
	}
}

func TestProfiles(t *testing.T) {
	baseline := Config{Profile: ProfileBaseline}
	if baseline.RuleEnabled(ProfileRestricted) {
		t.Fatal("baseline enabled a restricted rule")
	}
	if !Default().RuleEnabled(ProfileRestricted) {
		t.Fatal("default restricted profile did not enable restricted rule")
	}
}

func TestFingerprintIncludesCompletePolicyConfiguration(t *testing.T) {
	base := Default()
	changed := Default()
	changed.Exclusions.Namespaces = []string{"kube-system"}
	if base.Fingerprint() == "" || base.Fingerprint() == changed.Fingerprint() {
		t.Fatalf("policy fingerprints = %q and %q", base.Fingerprint(), changed.Fingerprint())
	}
}
