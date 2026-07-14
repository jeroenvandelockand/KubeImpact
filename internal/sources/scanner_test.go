package sources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

func TestDirectoryScanPreservesManifestAPIVersionAndWorkloads(t *testing.T) {
	root := t.TempDir()
	manifestDir := filepath.Join(root, "manifests")
	if err := os.Mkdir(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: payments
spec:
  selector:
    matchLabels: {app: api}
  template:
    metadata:
      labels: {app: api}
    spec:
      containers:
        - name: api
          image: example/api:1
---
apiVersion: storagemigration.k8s.io/v1alpha1
kind: StorageVersionMigration
metadata:
  name: legacy
`
	if err := os.WriteFile(filepath.Join(manifestDir, "resources.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	scanner, err := New(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceDirectory, Path: "manifests"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Deployments) != 1 || len(snapshot.Resources) != 2 || len(snapshot.SourceResults) != 1 || snapshot.SourceResults[0].Documents != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Resources[1].ObservedAPIVersions[0] != "storagemigration.k8s.io/v1alpha1" {
		t.Fatalf("resource = %#v", snapshot.Resources[1])
	}
	if source := collector.ObjectSource(snapshot.Deployments[0].Annotations); !strings.Contains(source, "resources.yaml") {
		t.Fatalf("deployment source = %q", source)
	}
}

func TestDirectoryScanRejectsEscapesAndWarnsForTemplates(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("apiVersion: v1\nkind: Service\nmetadata: {name: outside}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceDirectory, Path: outside}}); err == nil {
		t.Fatal("scan accepted path outside source root")
	}

	if err := os.WriteFile(filepath.Join(root, "template.yaml"), []byte("apiVersion: {{ .Values.apiVersion }}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Warnings) != 1 || !strings.Contains(snapshot.Warnings[0], "unrendered content") {
		t.Fatalf("warnings = %v", snapshot.Warnings)
	}
}

func TestDirectoryScanDoesNotMistakeLiteralTemplateTextForHelm(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: templates
data:
  greeting: "Hello {{ user }}"
`
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceDirectory, Path: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Resources) != 1 || len(snapshot.Warnings) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestHelmSourceUsesRenderedOutput(t *testing.T) {
	root := t.TempDir()
	chart := filepath.Join(root, "chart")
	if err := os.Mkdir(chart, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "rendered.yaml")
	if err := os.WriteFile(fixture, []byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: edge\nspec:\n  externalIPs: [203.0.113.10]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	helm := filepath.Join(root, "helm-test")
	if err := os.WriteFile(helm, []byte(fmt.Sprintf("#!/bin/sh\ncat %q\n", fixture)), 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Root: root, HelmBinary: helm})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceHelm, Path: "chart", ReleaseName: "edge", Namespace: "gateway"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Services) != 1 || snapshot.SourceResults[0].Type != models.SourceHelm {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if location := snapshot.SourceResults[0].Location; !strings.Contains(location, "release=edge") || !strings.Contains(location, "namespace=gateway") {
		t.Fatalf("Helm source location = %q", location)
	}
}

func TestGitSourceClonesAllowlistedRepository(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(fixture, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata: {name: agent, namespace: monitoring}\nspec:\n  selector:\n    matchLabels: {app: agent}\n  template:\n    metadata:\n      labels: {app: agent}\n    spec:\n      containers: [{name: agent, image: example/agent:1}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(root, "git-test")
	script := fmt.Sprintf("#!/bin/sh\nfor last; do :; done\nmkdir -p \"$last/deploy\"\ncp %q \"$last/deploy/manifest.yaml\"\n", fixture)
	if err := os.WriteFile(git, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{GitBinary: git, AllowedGitHosts: []string{"github.com"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceGit, URL: "https://github.com/example/repo.git", Ref: "main", Path: "deploy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DaemonSets) != 1 || snapshot.SourceResults[0].Type != models.SourceGit || !strings.Contains(snapshot.SourceResults[0].Location, "#deploy") {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	blocked, _ := New(Config{GitBinary: git})
	if _, err := blocked.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceGit, URL: "https://github.com/example/repo.git"}}); err == nil {
		t.Fatal("scan accepted Git host without allowlist")
	}
}

func TestValidateSpecRejectsUnsafeAndAmbiguousOptions(t *testing.T) {
	for _, spec := range []models.SourceSpec{
		{Type: models.SourceDirectory},
		{Type: models.SourceHelm, Path: "chart", ReleaseName: "--post-renderer"},
		{Type: models.SourceGit, URL: "https://token:secret@github.com/example/repo.git"},
		{Type: models.SourceGit, URL: "https://github.com/example/repo.git", Path: "manifests", ChartPath: "chart"},
		{Type: models.SourceGit, URL: "https://github.com/example/repo.git", ValuesFiles: []string{"values.yaml"}},
	} {
		if err := ValidateSpec(spec); err == nil {
			t.Errorf("ValidateSpec(%#v) returned no error", spec)
		}
	}
}

func TestHelmSourceRejectsChartSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	chart := filepath.Join(root, "chart")
	if err := os.Mkdir(chart, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(chart, "escape")); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Root: root, HelmBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceHelm, Path: "chart"}}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Helm symlink scan error = %v", err)
	}
}

func TestGitSourceEnforcesRepositorySizeLimit(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "large.yaml")
	if err := os.WriteFile(fixture, []byte(strings.Repeat("x", 128)), 0o600); err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(root, "git-test")
	script := fmt.Sprintf("#!/bin/sh\nfor last; do :; done\nmkdir -p \"$last\"\ncp %q \"$last/large.yaml\"\n", fixture)
	if err := os.WriteFile(git, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{GitBinary: git, AllowedGitHosts: []string{"github.com"}, MaxRepoBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(context.Background(), []models.SourceSpec{{Type: models.SourceGit, URL: "https://github.com/example/repo.git"}}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized Git repository error = %v", err)
	}
}

func TestSSHCommandUsesIsolatedKnownHostsCopy(t *testing.T) {
	root := t.TempDir()
	knownHosts := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("github.com ssh-ed25519 test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{SSHKnownHosts: knownHosts})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "isolated")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := scanner.sshCommand(home)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(home, "known_hosts")
	if !strings.Contains(command, "StrictHostKeyChecking=yes") || !strings.Contains(command, copyPath) {
		t.Fatalf("SSH command = %q", command)
	}
	data, err := os.ReadFile(copyPath)
	if err != nil || !strings.Contains(string(data), "test-key") {
		t.Fatalf("known_hosts copy = %q, %v", data, err)
	}
}
