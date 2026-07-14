package analyzers

import (
	"kubeimpact/internal/analyzers/upgrade"
	"kubeimpact/internal/analyzers/workload"
)

func Default(targetVersion string) []Analyzer {
	return []Analyzer{
		workload.New(),
		upgrade.New(targetVersion),
	}
}
