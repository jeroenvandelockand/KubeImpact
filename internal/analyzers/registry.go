package analyzers

import (
	"kubeimpact/internal/analyzers/upgrade"
	"kubeimpact/internal/analyzers/workload"
	"kubeimpact/internal/policy"
)

func Default(targetVersion string, config policy.Config) []Analyzer {
	return []Analyzer{
		workload.New(config),
		upgrade.New(targetVersion),
	}
}
