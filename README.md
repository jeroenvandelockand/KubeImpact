# KubeImpact

KubeImpact combines live Kubernetes evidence with raw manifests, rendered Helm charts, and allowlisted Git repositories to identify workload risks and upgrade blockers before a cluster upgrade.

Scans are asynchronous and persistent:

1. `POST /api/v1/scans` stores a pending scan in SQLite and returns `202 Accepted` with a scan ID.
2. A single worker collects the requested evidence and runs the analyzers.
3. `GET /api/v1/scans/:id` reports pending, running, completed, or failed status.
4. Completed reports remain available through `GET /api/v1/report/latest` and `GET /api/v1/reports` after service restarts.
5. A report is compared with the previous report that used the same target, complete policy configuration, cluster/source scope, and source configuration. Signals are classified as new, unchanged, or resolved.

## Why multiple evidence sources matter

Live Kubernetes objects are served through the API version requested by the reader, so reading the cluster alone cannot reliably prove which API version still exists in source manifests or which version clients call.

KubeImpact uses three complementary forms of evidence:

- **Declared state:** raw YAML/JSON preserves the exact `apiVersion` committed by a team.
- **Rendered state:** `helm template` exposes the resources that a chart and its selected values actually produce.
- **Runtime use:** `apiserver_requested_deprecated_apis` identifies deprecated API versions requested since the API server started.

Live-object `metadata.managedFields[].apiVersion` remains a useful fallback, but reports clearly warn that it is not exhaustive.

## Checks

The baseline workload profile checks:

- Host network, PID, and IPC namespaces
- Privileged and Windows HostProcess containers
- Host ports and hostPath volumes
- Unsafe sysctls

The restricted profile includes baseline and also checks:

- Privilege escalation
- Non-root enforcement
- Dropping all Linux capabilities and restricting added capabilities to the Pod Security Standards allowlists
- RuntimeDefault or Localhost seccomp profiles
- Read-only root filesystems
- Automatic ServiceAccount token mounting
- CPU and memory requests and memory limits

Deployments, StatefulSets, DaemonSets, regular containers, init containers, and ephemeral containers are covered. Each signal includes a stable fingerprint, evidence source, resource/container identity, field path, current and expected values, remediation, and documentation.

Upgrade analysis currently includes the Kubernetes 1.35 `StorageVersionMigration` v1alpha1 removal, the Kubernetes 1.36 Service `spec.externalIPs` deprecation, exact manifest `apiVersion` evidence, managed-fields fallback evidence, and API-server deprecated-request metrics.

## Requirements

- Go 1.26.4 or newer
- Node.js 24 and npm for dashboard development
- `git` for Git sources
- Helm 3 for Helm sources
- Kubernetes credentials for cluster-inclusive scans

The container image includes Git and Helm.

## Run locally

Start the API:

```bash
export KUBEIMPACT_SOURCE_ROOT="$PWD"
export KUBEIMPACT_GIT_HOSTS="github.com,gitlab.com"
go run ./cmd/api
```

The SQLite database is created at `data/kubeimpact.db` by default.

Start the dashboard in another terminal:

```bash
cd web
npm ci
npm run dev
```

Vite proxies `/api` to `http://localhost:8080`. Open `http://localhost:5173`.

## API

### Start a cluster scan

```bash
curl -X POST http://localhost:8080/api/v1/scans \
  -H 'Content-Type: application/json' \
  -d '{
    "targetVersion": "1.36",
    "includeCluster": true
  }'
```

The response is `202 Accepted`:

```json
{
  "id": "de8cfeab-6fb9-4d95-a305-e5be84d18f54",
  "status": "pending",
  "request": {
    "targetVersion": "1.36",
    "includeCluster": true
  },
  "createdAt": "2026-07-14T14:00:00Z"
}
```

`POST /api/v1/scan` remains as a compatibility alias and has the same asynchronous behavior.

### Poll scan status

```bash
curl http://localhost:8080/api/v1/scans/de8cfeab-6fb9-4d95-a305-e5be84d18f54
```

A completed record contains its report. A failed record contains a bounded error message. Pending/running scans interrupted by a restart are marked failed when the service starts again.

### Latest report and history

```bash
curl http://localhost:8080/api/v1/report/latest
curl 'http://localhost:8080/api/v1/reports?limit=20'
```

Before the first completed scan, the latest endpoint returns `404`. Both endpoints read SQLite and never trigger a Kubernetes scan.

### Scan raw manifests

Local paths must resolve beneath `KUBEIMPACT_SOURCE_ROOT`:

```bash
curl -X POST http://localhost:8080/api/v1/scans \
  -H 'Content-Type: application/json' \
  -d '{
    "targetVersion": "1.36",
    "includeCluster": true,
    "sources": [
      {"type": "directory", "path": "payments/manifests"}
    ]
  }'
```

A path may identify one YAML/JSON file or a directory. Directories are scanned recursively with deterministic ordering. `.git`, `node_modules`, `vendor`, and `.terraform` are ignored. Symlink/path escapes, oversized files, excessive file counts, malformed documents, and resources without `apiVersion`, `kind`, or a name fail the scan instead of producing a false-clean result. A request may include at most 20 distinct sources.

Files that fail decoding because they contain unrendered `{{ ... }}` content are skipped with a report warning; valid documents before the templated content are retained. Literal template text inside otherwise valid YAML remains analyzable. Configure a Helm source to render the complete file.

### Render a local Helm chart

```json
{
  "targetVersion": "1.36",
  "includeCluster": true,
  "sources": [
    {
      "type": "helm",
      "path": "charts/payments",
      "releaseName": "payments",
      "namespace": "payments",
      "valuesFiles": ["charts/payments/values-prod.yaml"]
    }
  ]
}
```

KubeImpact runs Helm with an argument array, `--include-crds`, and `--skip-tests`. Release and namespace values are validated before Helm starts. Chart contents and values paths are constrained to the source root, including symlink targets. Helm runs with isolated configuration/plugin directories, and rendered and error output are size-limited before being decoded in memory.

### Scan an allowlisted Git repository

Remote Git is disabled until `KUBEIMPACT_GIT_HOSTS` is configured. Only `https://`, `ssh://`, and `git@host:path` URLs are accepted. Allowlist entries match the exact host and optional port, so `git.example.com:2222` must be listed separately from `git.example.com`.

```json
{
  "targetVersion": "1.36",
  "includeCluster": true,
  "sources": [
    {
      "type": "git",
      "url": "https://github.com/example/platform-manifests.git",
      "ref": "main",
      "path": "deploy/production"
    }
  ]
}
```

`path` optionally selects one manifest file or subdirectory; omit it to scan the repository. Git sources are shallow-cloned into an isolated temporary directory with terminal prompts, hooks, inherited Git configuration, cross-host HTTP redirects, and local/external Git protocols disabled. Repository file count and on-disk size are bounded after clone, while the deployment also caps temporary storage. Credential-bearing HTTPS URLs are rejected so secrets cannot enter persisted scan requests; use an SSH agent and a trusted `known_hosts` file for private repositories. A Git repository containing a Helm chart can be rendered directly:

```json
{
  "type": "git",
  "url": "git@github.com:example/charts.git",
  "ref": "v2.4.0",
  "chartPath": "charts/payments",
  "releaseName": "payments",
  "namespace": "payments",
  "valuesFiles": ["environments/prod.yaml"]
}
```

### Manifest-only scan

When cluster collection is disabled, `currentVersion` is required so KubeImpact can build the complete upgrade path:

```json
{
  "currentVersion": "1.35",
  "targetVersion": "1.36",
  "includeCluster": false,
  "sources": [
    {"type": "directory", "path": "manifests"}
  ]
}
```

## Policy profiles, exclusions, and suppressions

The default profile is `restricted`. Set `KUBEIMPACT_POLICY_FILE` to a strict YAML configuration; unknown fields and invalid severities stop startup.

See [`policy.example.yaml`](policy.example.yaml):

```yaml
profile: restricted

exclusions:
  namespaces:
    - "kube-system"
    - "vendor-*"
  workloadLabels:
    kubeimpact.io/exclude: "true"
  namespaceLabels:
    kubeimpact.io/exclude: "true"

severityOverrides:
  WLK-HOSTNET-001: high
```

Namespace exclusions support shell-style globs. All configured workload-label or namespace-label pairs must match for that exclusion to apply.

Suppress individual rules on a workload or its Pod template:

```yaml
metadata:
  annotations:
    kubeimpact.io/ignore: "WLK-HOSTNET-001,WLK-HOSTPORT-001"
    kubeimpact.io/ignore-reason: "Reviewed node-local ingress component; exception expires 2026-12-01."
```

`kubeimpact.io/ignore: "*"` is supported but still requires a reason. A suppression without `kubeimpact.io/ignore-reason` is rejected and produces `WLK-SUPPRESSION-INVALID-001`. Valid suppressions are stored in the report with their reason and original finding fingerprint.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `KUBEIMPACT_ADDR` | `:8080` | API listen address |
| `KUBEIMPACT_SCAN_TIMEOUT` | `60s` | Maximum worker time per scan |
| `KUBEIMPACT_DB_PATH` | `data/kubeimpact.db` | SQLite database path |
| `KUBEIMPACT_POLICY_FILE` | empty | Optional policy YAML file |
| `KUBEIMPACT_SOURCE_ROOT` | empty | Allowed root for local directory/Helm sources; empty disables them |
| `KUBEIMPACT_GIT_HOSTS` | empty | Comma-separated remote Git host allowlist; empty disables remote Git |
| `KUBEIMPACT_SSH_KNOWN_HOSTS` | empty | Optional trusted SSH `known_hosts` file copied into each isolated Git workspace |
| `KUBEIMPACT_WEB_DIR` | `/web` | Built dashboard directory |
| `KUBEIMPACT_CORS_ORIGIN` | empty | One exact separately hosted dashboard origin |
| `KUBECONFIG` | client-go default | Local Kubernetes credentials |
| `VITE_API_BASE_URL` | empty | Dashboard API origin when hosted separately |

When UI and API share an origin, leave CORS disabled. Do not expose the API publicly without authentication and authorization: reports contain cluster/source metadata and scan requests can cause network and filesystem reads within configured boundaries.

## Container image

Build and run with persistent data and an optional read-only source tree:

```bash
docker build -t kubeimpact:dev .
mkdir -p data
docker run --rm -p 8080:8080 \
  --user "$(id -u):$(id -g)" \
  -e KUBECONFIG=/config/kubeconfig \
  -e KUBEIMPACT_SOURCE_ROOT=/sources \
  -e KUBEIMPACT_GIT_HOSTS=github.com \
  -v "$HOME/.kube/config:/config/kubeconfig:ro" \
  -v "$PWD/data:/data" \
  -v "$PWD:/sources:ro" \
  kubeimpact:dev
```

Open `http://localhost:8080`.

## Kubernetes deployment

Edit the image in [`deploy/kubernetes.yaml`](deploy/kubernetes.yaml), then apply it:

```bash
kubectl apply -f deploy/kubernetes.yaml
kubectl -n kube-system port-forward service/kubeimpact 8080:80
```

The manifest includes:

- Read-only RBAC for workloads, Services, Namespaces, the currently modeled removed API, and `/metrics`
- A non-root, read-only-root-filesystem container
- A writable, 512 MiB-capped temporary `emptyDir` for Git clones
- A 1 GiB persistent volume claim for SQLite
- One replica, one scan worker, filesystem-group ownership for the PVC, and a `Recreate` rollout strategy that prevents two SQLite writers

Keep one replica while using local SQLite/RWO storage. Moving to multiple replicas requires a shared database and a distributed job claim/lease mechanism.

To scan local files in Kubernetes, mount a read-only volume at `/sources` and set `KUBEIMPACT_SOURCE_ROOT=/sources`. Configure `KUBEIMPACT_GIT_HOSTS` explicitly for remote repositories.

## Rules and upgrade paths

Versioned rules live under [`rules/kubernetes`](rules/kubernetes) and are embedded in the Go binary. Unknown YAML fields, missing required values, invalid severities, mismatched versions, unknown evaluators, and duplicate IDs stop startup.

Upgrade analysis loads every rule file between the current and requested target minor versions. Upgrading from 1.34 to 1.36 evaluates both 1.35 and 1.36.

Kubernetes 1.37 is a prerelease target and intentionally contains only verified, machine-detectable entries.

## Scoring and comparisons

Reports start at 100 and apply severity weights of 25/10/5/2 for critical/high/medium/low. Penalties are capped per severity at 50/30/15/5 so cluster size cannot create an unbounded penalty. `scoreBreakdown` exposes every applied cap and penalty.

Comparison uses stable signal fingerprints and only a compatible previous scan. A change in target, complete policy configuration, cluster inclusion, current version for manifest-only scans, or source configuration starts a new comparison baseline rather than incorrectly reporting everything resolved.

Treat the score as prioritization help, not an upgrade guarantee.

## Limitations

- SQLite and the in-process worker intentionally support one application replica.
- Git branch/tag shallow clones are supported; arbitrary commit-only refs may require a branch or tag.
- API-server metrics describe API use since that API server process started and depend on `/metrics` RBAC.
- Managed-fields evidence can be incomplete; source manifests and request metrics are stronger signals.
- Kustomize rendering is not implemented yet. Render Kustomize output to a directory/file source as a workaround.
- The rule corpus intentionally includes only verified machine-detectable changes; operators must still review official release notes.

## Development checks

```bash
gofmt -w cmd internal rules
go vet ./cmd/... ./internal/... ./rules/...
go test -race ./cmd/... ./internal/... ./rules/...

cd web
npm run lint
npm run build
```

The scoped Go package patterns avoid traversing JavaScript dependencies under `web/node_modules`.
