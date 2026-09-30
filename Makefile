SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

CLUSTER_NAME ?= kubeimpact-demo
export CLUSTER_NAME

.PHONY: help doctor demo cluster image deploy wait scan report remediate reset-fixture status test test-upgrade test-remediation test-persistence diagnostics validate destroy clean

help: ## Show available demo commands
	@awk 'BEGIN {FS = ":.*## "; printf "\nKubeImpact Kubernetes upgrade demo\n\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doctor: ## Check the local prerequisites
	@./scripts/doctor.sh

demo: doctor cluster image deploy wait ## Build the demo and run a real 1.36-to-1.37 scan
	@./scripts/scan.sh initial
	@./scripts/report.sh initial
	@printf '\nKubeImpact upgrade demo READY\nRun "make test" to prove detection, remediation, comparison, and restart persistence.\n\n'

cluster: ## Create the pinned Kubernetes 1.36 Kind cluster
	@./scripts/create-cluster.sh

image: ## Build kubeimpact:demo and load it into Kind
	@./scripts/build-image.sh

deploy: ## Deploy KubeImpact and the blocked upgrade fixture
	@./scripts/deploy.sh

wait: ## Wait for the API to become ready (bounded)
	@./scripts/wait.sh

scan: ## Queue and wait for an asynchronous scan of the current fixture
	@./scripts/scan.sh manual

report: ## Show the latest saved scan report
	@./scripts/report.sh

remediate: ## Replace the fixture in place, restart, and scan it again
	@./scripts/remediate.sh
	@./scripts/scan.sh remediated
	@./scripts/report.sh remediated

reset-fixture: ## Restore the blocked 1.37 upgrade fixture in place
	@./scripts/reset-fixture.sh

status: ## Show cluster, deployment, API, and latest-report status
	@./scripts/status.sh

test: ## Prove detection, remediation/resolution, and SQLite persistence
	@./tests/all.sh

test-upgrade: ## Check the exact configured Kubernetes 1.37 rule IDs
	@./tests/upgrade.sh

test-remediation: ## Remediate through the same source request and check resolved signals
	@./tests/remediation.sh

test-persistence: ## Restart the pod and prove reports remain available
	@./tests/persistence.sh

diagnostics: ## Collect bounded failure diagnostics
	@./scripts/diagnostics.sh

validate: ## Run offline syntax and manifest checks
	@./scripts/validate.sh

destroy: ## Delete only the named demo cluster and its runtime files
	@./scripts/destroy.sh

clean: destroy ## Alias for destroy
