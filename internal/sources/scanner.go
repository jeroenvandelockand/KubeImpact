package sources

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"kubeimpact/internal/collector"
	"kubeimpact/internal/models"
)

const (
	defaultMaxFiles      = 5000
	defaultMaxFileBytes  = 10 << 20
	defaultMaxOutputByte = 50 << 20
	defaultMaxRepoFiles  = 50000
	defaultMaxRepoBytes  = 250 << 20
	defaultMaxErrorBytes = 1 << 20
)

var sshUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var errInvalidManifest = errors.New("invalid Kubernetes manifest")

type Config struct {
	Root            string
	AllowedGitHosts []string
	GitBinary       string
	HelmBinary      string
	SSHKnownHosts   string
	MaxFiles        int
	MaxFileBytes    int64
	MaxOutputBytes  int64
	MaxRepoFiles    int
	MaxRepoBytes    int64
}

type Scanner struct {
	config Config
	root   string
	hosts  map[string]bool
}

func New(config Config) (*Scanner, error) {
	if config.GitBinary == "" {
		config.GitBinary = "git"
	}
	if config.HelmBinary == "" {
		config.HelmBinary = "helm"
	}
	if config.MaxFiles <= 0 {
		config.MaxFiles = defaultMaxFiles
	}
	if config.MaxFileBytes <= 0 {
		config.MaxFileBytes = defaultMaxFileBytes
	}
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = defaultMaxOutputByte
	}
	if config.MaxRepoFiles <= 0 {
		config.MaxRepoFiles = defaultMaxRepoFiles
	}
	if config.MaxRepoBytes <= 0 {
		config.MaxRepoBytes = defaultMaxRepoBytes
	}

	root := ""
	if config.Root != "" {
		absolute, err := filepath.Abs(config.Root)
		if err != nil {
			return nil, fmt.Errorf("resolve source root: %w", err)
		}
		root = filepath.Clean(absolute)
	}
	hosts := make(map[string]bool)
	for _, host := range config.AllowedGitHosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			hosts[host] = true
		}
	}
	return &Scanner{config: config, root: root, hosts: hosts}, nil
}

func (s *Scanner) Scan(ctx context.Context, specs []models.SourceSpec) (*collector.Snapshot, error) {
	combined := emptySnapshot()
	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := ValidateSpec(spec); err != nil {
			return nil, err
		}
		var snapshot *collector.Snapshot
		var result models.SourceResult
		var err error
		switch spec.Type {
		case models.SourceDirectory:
			snapshot, result, err = s.scanDirectory(ctx, spec)
		case models.SourceHelm:
			snapshot, result, err = s.scanHelm(ctx, spec, "", "helm:")
		case models.SourceGit:
			snapshot, result, err = s.scanGit(ctx, spec)
		default:
			err = fmt.Errorf("unsupported source type %q", spec.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("scan %s source: %w", spec.Type, err)
		}
		collector.Merge(combined, snapshot)
		combined.SourceResults = append(combined.SourceResults, result)
	}
	return combined, nil
}

// ValidateSpec rejects malformed or ambiguous source configurations before any
// filesystem access or external command is attempted.
func ValidateSpec(spec models.SourceSpec) error {
	switch spec.Type {
	case models.SourceDirectory:
		if strings.TrimSpace(spec.Path) == "" {
			return errors.New("directory source path is required")
		}
	case models.SourceHelm:
		if strings.TrimSpace(spec.Path) == "" {
			return errors.New("Helm chart path is required")
		}
		if err := validateHelmOptions(spec); err != nil {
			return err
		}
	case models.SourceGit:
		if _, _, err := gitHost(spec.URL); err != nil {
			return err
		}
		if spec.Path != "" && spec.ChartPath != "" {
			return errors.New("Git source cannot set both path and chartPath")
		}
		if spec.ChartPath == "" && (spec.ReleaseName != "" || spec.Namespace != "" || len(spec.ValuesFiles) > 0) {
			return errors.New("Git Helm options require chartPath")
		}
		if spec.ChartPath != "" {
			if err := validateHelmOptions(spec); err != nil {
				return err
			}
		}
		if strings.ContainsAny(spec.Ref, "\r\n\x00") || len(spec.Ref) > 255 {
			return errors.New("Git ref is invalid")
		}
	default:
		return fmt.Errorf("unsupported source type %q", spec.Type)
	}
	return nil
}

func validateHelmOptions(spec models.SourceSpec) error {
	if release := strings.TrimSpace(spec.ReleaseName); release != "" {
		if len(release) > 53 || len(validation.IsDNS1123Subdomain(release)) > 0 {
			return errors.New("Helm releaseName must be a DNS-1123 name no longer than 53 characters")
		}
	}
	if namespace := strings.TrimSpace(spec.Namespace); namespace != "" && len(validation.IsDNS1123Label(namespace)) > 0 {
		return errors.New("Helm namespace must be a DNS-1123 label")
	}
	return nil
}

func (s *Scanner) scanDirectory(ctx context.Context, spec models.SourceSpec) (*collector.Snapshot, models.SourceResult, error) {
	resolved, err := s.resolveLocalPath(spec.Path)
	if err != nil {
		return nil, models.SourceResult{}, err
	}
	location := relativeLocation(s.root, resolved)
	scope := "directory:" + location
	snapshot, documents, warnings, err := s.scanPath(ctx, resolved, scope, resolved)
	return snapshot, models.SourceResult{Type: models.SourceDirectory, Location: location, Documents: documents, Resources: len(snapshotResources(snapshot)), Warnings: warnings}, err
}

func (s *Scanner) scanHelm(ctx context.Context, spec models.SourceSpec, trustedRoot, sourcePrefix string) (*collector.Snapshot, models.SourceResult, error) {
	chartPath := spec.Path
	var resolved string
	var err error
	if trustedRoot == "" {
		resolved, err = s.resolveLocalPath(chartPath)
	} else {
		resolved, err = resolveWithin(trustedRoot, chartPath)
	}
	if err != nil {
		return nil, models.SourceResult{}, err
	}
	containmentRoot := firstNonEmpty(trustedRoot, s.root)
	if err := ensureTreeContained(containmentRoot, resolved); err != nil {
		return nil, models.SourceResult{}, err
	}

	release := strings.TrimSpace(spec.ReleaseName)
	if release == "" {
		release = "kubeimpact-scan"
	}
	args := []string{"template", release, resolved, "--include-crds", "--skip-tests"}
	if spec.Namespace != "" {
		args = append(args, "--namespace", spec.Namespace)
	}
	valueLocations := make([]string, 0, len(spec.ValuesFiles))
	for _, valuesFile := range spec.ValuesFiles {
		var valuesPath string
		if trustedRoot == "" {
			valuesPath, err = s.resolveLocalPath(valuesFile)
		} else {
			valuesPath, err = resolveWithin(trustedRoot, valuesFile)
		}
		if err != nil {
			return nil, models.SourceResult{}, fmt.Errorf("resolve values file %q: %w", valuesFile, err)
		}
		args = append(args, "--values", valuesPath)
		valueLocations = append(valueLocations, relativeLocation(containmentRoot, valuesPath))
	}

	helmHome, err := os.MkdirTemp("", "kubeimpact-helm-")
	if err != nil {
		return nil, models.SourceResult{}, fmt.Errorf("create Helm workspace: %w", err)
	}
	defer os.RemoveAll(helmHome)
	command := exec.CommandContext(ctx, s.config.HelmBinary, args...)
	command.Env = isolatedEnvironment(helmHome, "HELM_", []string{
		"HELM_CACHE_HOME=" + filepath.Join(helmHome, "cache"),
		"HELM_CONFIG_HOME=" + filepath.Join(helmHome, "config"),
		"HELM_DATA_HOME=" + filepath.Join(helmHome, "data"),
		"HELM_PLUGINS=" + filepath.Join(helmHome, "plugins"),
		"KUBECONFIG=/dev/null",
	})
	output := &limitedBuffer{limit: s.config.MaxOutputBytes, description: "rendered output"}
	command.Stdout = output
	stderr := &limitedBuffer{limit: defaultMaxErrorBytes, description: "Helm error output"}
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, models.SourceResult{}, errors.New("helm executable is not installed")
		}
		return nil, models.SourceResult{}, fmt.Errorf("helm template failed: %s", sanitizeCommandError(stderr.String(), err))
	}

	location := helmLocation(relativeLocation(containmentRoot, resolved), release, spec.Namespace, valueLocations)
	label := sourcePrefix + location
	snapshot, documents, err := decodeDocuments(bytes.NewReader(output.Bytes()), label, label)
	result := models.SourceResult{Type: models.SourceHelm, Location: location, Documents: documents, Resources: len(snapshotResources(snapshot)), Warnings: []string{}}
	return snapshot, result, err
}

func (s *Scanner) scanGit(ctx context.Context, spec models.SourceSpec) (*collector.Snapshot, models.SourceResult, error) {
	host, sanitized, err := gitHost(spec.URL)
	if err != nil {
		return nil, models.SourceResult{}, err
	}
	if !s.hosts[strings.ToLower(host)] {
		return nil, models.SourceResult{}, fmt.Errorf("Git host %q is not allowed; configure KUBEIMPACT_GIT_HOSTS", host)
	}

	temporary, err := os.MkdirTemp("", "kubeimpact-git-")
	if err != nil {
		return nil, models.SourceResult{}, fmt.Errorf("create Git workspace: %w", err)
	}
	defer os.RemoveAll(temporary)
	repository := filepath.Join(temporary, "repository")
	home := filepath.Join(temporary, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, models.SourceResult{}, fmt.Errorf("create isolated Git home: %w", err)
	}
	sshCommand, err := s.sshCommand(home)
	if err != nil {
		return nil, models.SourceResult{}, err
	}
	args := []string{
		"-c", "http.followRedirects=false",
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ext.allow=never",
		"clone", "--depth", "1", "--single-branch", "--config", "core.hooksPath=/dev/null",
	}
	if spec.Ref != "" {
		args = append(args, "--branch", spec.Ref)
	}
	args = append(args, spec.URL, repository)
	command := exec.CommandContext(ctx, s.config.GitBinary, args...)
	command.Env = isolatedEnvironment(home, "GIT_", []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_ASKPASS=/bin/false",
		"GIT_SSH_COMMAND=" + sshCommand,
		"SSH_ASKPASS=/bin/false",
	})
	stderr := &limitedBuffer{limit: defaultMaxErrorBytes, description: "Git error output"}
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, models.SourceResult{}, errors.New("git executable is not installed")
		}
		message := sanitizeCommandError(stderr.String(), err)
		message = strings.ReplaceAll(message, spec.URL, sanitized)
		return nil, models.SourceResult{}, fmt.Errorf("git clone failed: %s", message)
	}
	if err := validateRepositorySize(repository, s.config.MaxRepoFiles, s.config.MaxRepoBytes); err != nil {
		return nil, models.SourceResult{}, err
	}

	location := sanitized
	if spec.Ref != "" {
		location += "@" + spec.Ref
	}
	if spec.ChartPath != "" {
		helmSpec := spec
		helmSpec.Type = models.SourceHelm
		helmSpec.Path = spec.ChartPath
		snapshot, result, scanErr := s.scanHelm(ctx, helmSpec, repository, "git:"+location+"#")
		result.Type = models.SourceGit
		result.Location = location + "#" + result.Location
		return snapshot, result, scanErr
	}

	inputPath := repository
	resultLocation := location
	if spec.Path != "" {
		inputPath, err = resolveWithin(repository, spec.Path)
		if err != nil {
			return nil, models.SourceResult{}, fmt.Errorf("resolve Git source path %q: %w", spec.Path, err)
		}
		resultLocation += "#" + filepath.ToSlash(spec.Path)
	}
	scope := "git:" + resultLocation
	snapshot, documents, warnings, err := s.scanPath(ctx, inputPath, scope, inputPath)
	return snapshot, models.SourceResult{Type: models.SourceGit, Location: resultLocation, Documents: documents, Resources: len(snapshotResources(snapshot)), Warnings: warnings}, err
}

func (s *Scanner) sshCommand(home string) (string, error) {
	command := "ssh -oBatchMode=yes -oStrictHostKeyChecking=yes"
	if s.config.SSHKnownHosts == "" {
		return command, nil
	}
	info, err := os.Stat(s.config.SSHKnownHosts)
	if err != nil {
		return "", fmt.Errorf("read configured SSH known_hosts: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > defaultMaxErrorBytes {
		return "", errors.New("configured SSH known_hosts must be a regular file no larger than 1 MiB")
	}
	data, err := os.ReadFile(s.config.SSHKnownHosts)
	if err != nil {
		return "", fmt.Errorf("read configured SSH known_hosts: %w", err)
	}
	destination := filepath.Join(home, "known_hosts")
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return "", fmt.Errorf("prepare SSH known_hosts: %w", err)
	}
	return command + " -oUserKnownHostsFile=" + destination, nil
}

func (s *Scanner) scanPath(ctx context.Context, inputPath, sourcePrefix, relativeRoot string) (*collector.Snapshot, int, []string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, 0, nil, err
	}
	paths := []string{}
	if !info.IsDir() {
		paths = append(paths, inputPath)
	} else {
		err = filepath.WalkDir(inputPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && path != inputPath && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if entry.Type().IsRegular() && manifestExtension(path) {
				paths = append(paths, path)
				if len(paths) > s.config.MaxFiles {
					return fmt.Errorf("source contains more than %d manifest files", s.config.MaxFiles)
				}
			}
			return nil
		})
		if err != nil {
			return nil, 0, nil, err
		}
	}
	sort.Strings(paths)

	combined := emptySnapshot()
	documents := 0
	warnings := []string{}
	var totalBytes int64
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, 0, nil, statErr
		}
		if info.Size() > s.config.MaxFileBytes {
			return nil, 0, nil, fmt.Errorf("manifest %s exceeds %d bytes", path, s.config.MaxFileBytes)
		}
		totalBytes += info.Size()
		if totalBytes > s.config.MaxOutputBytes {
			return nil, 0, nil, fmt.Errorf("manifest source exceeds %d total bytes", s.config.MaxOutputBytes)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, 0, nil, readErr
		}
		relative := relativeLocation(relativeRoot, path)
		source := sourcePrefix
		if relative != "." {
			source += "#" + filepath.ToSlash(relative)
		}
		snapshot, count, decodeErr := decodeDocuments(bytes.NewReader(data), source, sourcePrefix)
		if decodeErr != nil {
			if bytes.Contains(data, []byte("{{")) && !errors.Is(decodeErr, errInvalidManifest) {
				warnings = append(warnings, fmt.Sprintf("Skipped unrendered content in %s after %d valid document(s); configure a Helm source with chartPath to render the complete file.", relative, count))
				documents += count
				collector.Merge(combined, snapshot)
				continue
			}
			return nil, 0, nil, fmt.Errorf("decode manifest %s: %w", relative, decodeErr)
		}
		documents += count
		collector.Merge(combined, snapshot)
	}
	combined.Warnings = append(combined.Warnings, warnings...)
	return combined, documents, warnings, nil
}

func decodeDocuments(reader io.Reader, source, sourceScope string) (*collector.Snapshot, int, error) {
	snapshot := emptySnapshot()
	decoder := utilyaml.NewYAMLOrJSONDecoder(reader, 4096)
	documents := 0
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return snapshot, documents, err
		}
		if len(object.Object) == 0 {
			continue
		}
		documents++
		if err := addObject(snapshot, object, source, sourceScope); err != nil {
			return snapshot, documents, fmt.Errorf("%w: %v", errInvalidManifest, err)
		}
	}
	return snapshot, documents, nil
}

func addObject(snapshot *collector.Snapshot, object unstructured.Unstructured, source, sourceScope string) error {
	if object.IsList() {
		items, err := object.ToList()
		if err != nil {
			return err
		}
		for _, item := range items.Items {
			if err := addObject(snapshot, item, source, sourceScope); err != nil {
				return err
			}
		}
		return nil
	}
	if object.GetAPIVersion() == "" || object.GetKind() == "" {
		return errors.New("manifest must contain apiVersion and kind")
	}
	if object.GetName() == "" {
		if object.GetGenerateName() == "" {
			return fmt.Errorf("%s %s has neither metadata.name nor metadata.generateName", object.GetAPIVersion(), object.GetKind())
		}
		object.SetName(object.GetGenerateName() + "<generated>")
	}
	if object.GetNamespace() == "" && isKnownNamespacedKind(object.GetKind()) {
		object.SetNamespace("default")
	}
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[collector.SourceAnnotation] = source
	annotations[collector.SourceScopeAnnotation] = sourceScope
	object.SetAnnotations(annotations)

	snapshot.Resources = append(snapshot.Resources, models.KubernetesResource{
		Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), Namespaced: object.GetNamespace() != "",
		ObservedAPIVersions: []string{object.GetAPIVersion()}, Source: source,
	})
	snapshot.Sources[collector.ResourceKey(object.GetKind(), object.GetNamespace(), object.GetName())] = source

	converter := runtime.DefaultUnstructuredConverter
	switch object.GetAPIVersion() + "/" + object.GetKind() {
	case "apps/v1/Deployment":
		var value appsv1.Deployment
		if err := converter.FromUnstructured(object.Object, &value); err != nil {
			return err
		}
		snapshot.Deployments = append(snapshot.Deployments, value)
	case "apps/v1/StatefulSet":
		var value appsv1.StatefulSet
		if err := converter.FromUnstructured(object.Object, &value); err != nil {
			return err
		}
		snapshot.StatefulSets = append(snapshot.StatefulSets, value)
	case "apps/v1/DaemonSet":
		var value appsv1.DaemonSet
		if err := converter.FromUnstructured(object.Object, &value); err != nil {
			return err
		}
		snapshot.DaemonSets = append(snapshot.DaemonSets, value)
	case "v1/Service":
		var value corev1.Service
		if err := converter.FromUnstructured(object.Object, &value); err != nil {
			return err
		}
		snapshot.Services = append(snapshot.Services, value)
	case "v1/Namespace":
		var value corev1.Namespace
		if err := converter.FromUnstructured(object.Object, &value); err != nil {
			return err
		}
		snapshot.Namespaces = append(snapshot.Namespaces, value)
	}
	return nil
}

func emptySnapshot() *collector.Snapshot {
	return &collector.Snapshot{
		Deployments: []appsv1.Deployment{}, StatefulSets: []appsv1.StatefulSet{}, DaemonSets: []appsv1.DaemonSet{},
		Services: []corev1.Service{}, Namespaces: []corev1.Namespace{}, Resources: []models.KubernetesResource{},
		DeprecatedAPIRequests: []models.DeprecatedAPIRequest{}, Sources: map[string]string{}, SourceResults: []models.SourceResult{}, Warnings: []string{},
	}
}

func (s *Scanner) resolveLocalPath(input string) (string, error) {
	if s.root == "" {
		return "", errors.New("local sources are disabled; configure KUBEIMPACT_SOURCE_ROOT")
	}
	return resolveWithin(s.root, input)
}

func resolveWithin(root, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", errors.New("source path is required")
	}
	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the configured source root", input)
	}
	return resolved, nil
}

func ensureTreeContained(root, input string) error {
	return filepath.WalkDir(input, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve chart symlink %q: %w", path, err)
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("chart symlink %q points outside the configured source root", path)
		}
		return nil
	})
}

func validateRepositorySize(root string, maxFiles int, maxBytes int64) error {
	files := 0
	var bytes int64
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		files++
		if files > maxFiles {
			return fmt.Errorf("Git repository contains more than %d files", maxFiles)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		bytes += info.Size()
		if bytes > maxBytes {
			return fmt.Errorf("Git repository exceeds %d bytes", maxBytes)
		}
		return nil
	})
}

func gitHost(rawURL string) (string, string, error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", "", errors.New("Git URL is required")
	}
	if rawURL != strings.TrimSpace(rawURL) || strings.ContainsAny(rawURL, "\r\n\x00") {
		return "", "", errors.New("Git URL is invalid")
	}
	if strings.HasPrefix(rawURL, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(rawURL, "git@"), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", errors.New("invalid Git SSH URL")
		}
		if err := validateGitHostname(parts[0]); err != nil {
			return "", "", err
		}
		host := strings.ToLower(parts[0])
		return host, host + "/" + parts[1], nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "https" && parsed.Scheme != "ssh" {
		return "", "", errors.New("Git URL must use https://, ssh://, or git@host:path syntax")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("Git URL must not contain a query or fragment")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return "", "", errors.New("Git URL must include a repository path")
	}
	if err := validateGitHostname(parsed.Hostname()); err != nil {
		return "", "", err
	}
	if parsed.Scheme == "https" && parsed.User != nil {
		return "", "", errors.New("Git HTTPS URL must not contain credentials; use an external credential mechanism")
	}
	if parsed.Scheme == "ssh" && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword || !sshUsernamePattern.MatchString(parsed.User.Username()) {
			return "", "", errors.New("Git SSH URL contains invalid user information")
		}
	}
	host := strings.ToLower(parsed.Host)
	parsed.User = nil
	return host, parsed.String(), nil
}

func validateGitHostname(host string) error {
	if net.ParseIP(host) != nil {
		return nil
	}
	if len(validation.IsDNS1123Subdomain(strings.ToLower(host))) > 0 {
		return errors.New("Git URL contains an invalid host")
	}
	return nil
}

func manifestExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".terraform":
		return true
	default:
		return false
	}
}

func isKnownNamespacedKind(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Service":
		return true
	default:
		return false
	}
}

func relativeLocation(root, path string) string {
	if root == "" {
		return filepath.ToSlash(path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func snapshotResources(snapshot *collector.Snapshot) []models.KubernetesResource {
	if snapshot == nil {
		return nil
	}
	return snapshot.Resources
}

func sanitizeCommandError(stderr string, fallback error) string {
	message := strings.TrimSpace(stderr)
	if message == "" {
		message = fallback.Error()
	}
	if len(message) > 1000 {
		message = message[:1000] + "…"
	}
	return message
}

func helmLocation(chart, release, namespace string, valuesFiles []string) string {
	parameters := url.Values{}
	parameters.Set("release", release)
	if namespace != "" {
		parameters.Set("namespace", namespace)
	}
	for _, valuesFile := range valuesFiles {
		parameters.Add("values", valuesFile)
	}
	return chart + "?" + parameters.Encode()
}

func isolatedEnvironment(home, removedPrefix string, additions []string) []string {
	environment := make([]string, 0, len(os.Environ())+len(additions)+2)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if name == "HOME" || name == "XDG_CONFIG_HOME" || name == "KUBECONFIG" || strings.HasPrefix(name, removedPrefix) {
			continue
		}
		environment = append(environment, value)
	}
	environment = append(environment, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"))
	return append(environment, additions...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type limitedBuffer struct {
	bytes.Buffer
	limit       int64
	description string
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if int64(b.Len()+len(data)) > b.limit {
		return 0, fmt.Errorf("%s exceeds %d bytes", b.description, b.limit)
	}
	return b.Buffer.Write(data)
}
