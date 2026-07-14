package knowledge

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"kubeimpact/internal/models"
	ruledata "kubeimpact/rules"
)

func LoadForVersion(version string) (*KubernetesRules, error) {
	normalized := NormalizeVersion(version)
	path := fmt.Sprintf("kubernetes/%s.yaml", normalized)

	data, err := fs.ReadFile(ruledata.Kubernetes, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("unsupported Kubernetes target version %q (supported: %s)", normalized, strings.Join(SupportedVersions(), ", "))
		}
		return nil, fmt.Errorf("read embedded rules for Kubernetes %s: %w", normalized, err)
	}

	rules, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if rules.Version != normalized {
		return nil, fmt.Errorf("%s declares version %q", path, rules.Version)
	}
	if err := validate(rules); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}

	return rules, nil
}

func decode(data []byte) (*KubernetesRules, error) {
	var rules KubernetesRules

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&rules); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not supported")
		}
		return nil, err
	}

	return &rules, nil
}

func validate(rules *KubernetesRules) error {
	if rules.Version == "" {
		return errors.New("version is required")
	}
	if _, _, err := parseVersion(rules.Version); err != nil {
		return err
	}

	ids := make(map[string]struct{})
	validateAPI := func(rule APIRule) error {
		if rule.ID == "" || rule.GroupVersion == "" || rule.Kind == "" || rule.RemovedIn == "" || rule.Message == "" || rule.Recommendation == "" || rule.DocumentationURL == "" {
			return fmt.Errorf("API rule %q has missing required fields", rule.ID)
		}
		if _, exists := ids[rule.ID]; exists {
			return fmt.Errorf("duplicate rule ID %q", rule.ID)
		}
		ids[rule.ID] = struct{}{}
		return nil
	}

	for _, rule := range rules.RemovedAPIs {
		if err := validateAPI(rule); err != nil {
			return err
		}
	}
	for _, rule := range rules.DeprecatedAPIs {
		if err := validateAPI(rule); err != nil {
			return err
		}
	}
	for _, check := range rules.ResourceChecks {
		if check.ID == "" || check.Name == "" || check.Message == "" || check.Recommendation == "" || check.DocumentationURL == "" {
			return fmt.Errorf("resource check %q has missing required fields", check.ID)
		}
		if _, exists := ids[check.ID]; exists {
			return fmt.Errorf("duplicate rule ID %q", check.ID)
		}
		ids[check.ID] = struct{}{}
		ids[check.ID] = struct{}{}
		if !models.ValidSeverity(models.Severity(check.Severity)) {
			return fmt.Errorf("resource check %q has invalid severity %q", check.ID, check.Severity)
		}
		switch check.Name {
		case "serviceExternalIPs":
		default:
			return fmt.Errorf("resource check %q uses unknown evaluator %q", check.ID, check.Name)
		}
	}

	return nil
}

func NormalizeVersion(version string) string {
	v := strings.TrimSpace(version)
	v = strings.TrimPrefix(v, "v")

	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return v
	}

	return parts[0] + "." + parts[1]
}

func SupportedVersions() []string {
	paths, err := fs.Glob(ruledata.Kubernetes, "kubernetes/*.yaml")
	if err != nil {
		return nil
	}

	versions := make([]string, 0, len(paths))
	for _, path := range paths {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "kubernetes/"), ".yaml")
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool {
		majorI, minorI, _ := parseVersion(versions[i])
		majorJ, minorJ, _ := parseVersion(versions[j])
		return majorI < majorJ || majorI == majorJ && minorI < minorJ
	})
	return versions
}

func IsSupportedVersion(version string) bool {
	normalized := NormalizeVersion(version)
	for _, supported := range SupportedVersions() {
		if supported == normalized {
			return true
		}
	}
	return false
}

func ValidateEmbedded() error {
	seenIDs := make(map[string]string)
	for _, version := range SupportedVersions() {
		rules, err := LoadForVersion(version)
		if err != nil {
			return err
		}
		for _, id := range ruleIDs(rules) {
			if previous, exists := seenIDs[id]; exists {
				return fmt.Errorf("rule ID %q is duplicated in Kubernetes %s and %s", id, previous, version)
			}
			seenIDs[id] = version
		}
	}
	return nil
}

func LoadForUpgrade(currentVersion, targetVersion string) ([]*KubernetesRules, error) {
	currentMajor, currentMinor, err := parseVersion(NormalizeVersion(currentVersion))
	if err != nil {
		return nil, fmt.Errorf("invalid current Kubernetes version %q: %w", currentVersion, err)
	}
	targetMajor, targetMinor, err := parseVersion(NormalizeVersion(targetVersion))
	if err != nil {
		return nil, fmt.Errorf("invalid target Kubernetes version %q: %w", targetVersion, err)
	}
	if currentMajor != targetMajor || targetMinor <= currentMinor {
		return nil, fmt.Errorf("target Kubernetes version %s must be newer than current version %s in the same major release", NormalizeVersion(targetVersion), NormalizeVersion(currentVersion))
	}

	rules := make([]*KubernetesRules, 0, targetMinor-currentMinor)
	for minor := currentMinor + 1; minor <= targetMinor; minor++ {
		version := fmt.Sprintf("%d.%d", targetMajor, minor)
		loaded, err := LoadForVersion(version)
		if err != nil {
			return nil, fmt.Errorf("upgrade path requires rules for Kubernetes %s: %w", version, err)
		}
		rules = append(rules, loaded)
	}
	return rules, nil
}

func ResourceSelectorsThrough(targetVersion string) ([]models.APIResourceSelector, error) {
	targetMajor, targetMinor, err := parseVersion(NormalizeVersion(targetVersion))
	if err != nil {
		return nil, err
	}
	if !IsSupportedVersion(targetVersion) {
		return nil, fmt.Errorf("unsupported Kubernetes target version %q (supported: %s)", NormalizeVersion(targetVersion), strings.Join(SupportedVersions(), ", "))
	}

	seen := make(map[models.APIResourceSelector]struct{})
	var selectors []models.APIResourceSelector
	for _, version := range SupportedVersions() {
		major, minor, parseErr := parseVersion(version)
		if parseErr != nil || major != targetMajor || minor > targetMinor {
			continue
		}
		rules, loadErr := LoadForVersion(version)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, rule := range append(append([]APIRule{}, rules.RemovedAPIs...), rules.DeprecatedAPIs...) {
			selector := models.APIResourceSelector{GroupVersion: rule.GroupVersion, Kind: rule.Kind}
			if _, exists := seen[selector]; exists {
				continue
			}
			seen[selector] = struct{}{}
			selectors = append(selectors, selector)
		}
	}
	return selectors, nil
}

func parseVersion(version string) (int, int, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("version %q must use major.minor format", version)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid major version in %q", version)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minor version in %q", version)
	}
	return major, minor, nil
}

func ruleIDs(rules *KubernetesRules) []string {
	ids := make([]string, 0, len(rules.RemovedAPIs)+len(rules.DeprecatedAPIs)+len(rules.ResourceChecks))
	for _, rule := range rules.RemovedAPIs {
		ids = append(ids, rule.ID)
	}
	for _, rule := range rules.DeprecatedAPIs {
		ids = append(ids, rule.ID)
	}
	for _, check := range rules.ResourceChecks {
		ids = append(ids, check.ID)
	}
	return ids
}
