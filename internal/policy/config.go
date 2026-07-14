package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"kubeimpact/internal/models"
)

type Profile string

const (
	ProfileBaseline   Profile = "baseline"
	ProfileRestricted Profile = "restricted"
)

type Config struct {
	Profile           Profile                    `yaml:"profile" json:"profile"`
	Exclusions        Exclusions                 `yaml:"exclusions" json:"exclusions"`
	SeverityOverrides map[string]models.Severity `yaml:"severityOverrides" json:"severityOverrides"`
}

type Exclusions struct {
	Namespaces      []string          `yaml:"namespaces" json:"namespaces"`
	WorkloadLabels  map[string]string `yaml:"workloadLabels" json:"workloadLabels"`
	NamespaceLabels map[string]string `yaml:"namespaceLabels" json:"namespaceLabels"`
}

func Default() Config {
	return Config{
		Profile:           ProfileRestricted,
		SeverityOverrides: map[string]models.Severity{},
	}
}

func Load(filePath string) (Config, error) {
	if filePath == "" {
		return Default(), nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return Config{}, fmt.Errorf("open policy file: %w", err)
	}
	defer file.Close()

	config := Default()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode policy file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("policy file contains multiple YAML documents")
		}
		return Config{}, fmt.Errorf("decode policy file: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.Profile != ProfileBaseline && c.Profile != ProfileRestricted {
		return fmt.Errorf("policy profile must be %q or %q", ProfileBaseline, ProfileRestricted)
	}
	for _, namespacePattern := range c.Exclusions.Namespaces {
		if namespacePattern == "" {
			return errors.New("namespace exclusion cannot be empty")
		}
		if _, err := path.Match(namespacePattern, "validation"); err != nil {
			return fmt.Errorf("invalid namespace exclusion %q: %w", namespacePattern, err)
		}
	}
	for ruleID, severity := range c.SeverityOverrides {
		if strings.TrimSpace(ruleID) == "" {
			return errors.New("severity override rule ID cannot be empty")
		}
		if !models.ValidSeverity(severity) {
			return fmt.Errorf("severity override for %s is invalid: %q", ruleID, severity)
		}
	}
	return nil
}

func (c Config) RuleEnabled(required Profile) bool {
	return required == ProfileBaseline || c.Profile == ProfileRestricted
}

func (c Config) Severity(ruleID string, fallback models.Severity) models.Severity {
	if severity, ok := c.SeverityOverrides[ruleID]; ok {
		return severity
	}
	return fallback
}

func (c Config) Fingerprint() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:16])
}

func (c Config) Excludes(namespace string, workloadLabels, namespaceLabels map[string]string) bool {
	for _, pattern := range c.Exclusions.Namespaces {
		if matched, _ := path.Match(pattern, namespace); matched {
			return true
		}
	}
	return labelsMatch(c.Exclusions.WorkloadLabels, workloadLabels) || labelsMatch(c.Exclusions.NamespaceLabels, namespaceLabels)
}

func labelsMatch(required, actual map[string]string) bool {
	if len(required) == 0 {
		return false
	}
	for key, value := range required {
		if actual[key] != value {
			return false
		}
	}
	return true
}
