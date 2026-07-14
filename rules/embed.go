package rules

import "embed"

// Kubernetes contains the versioned rule files shipped with the binary.
//
//go:embed kubernetes/*.yaml
var Kubernetes embed.FS
