# KubeImpact

KubeImpact scans a Kubernetes cluster for workload risks and machine-detectable upgrade concerns, then presents the latest report in a small web dashboard.

The scan lifecycle is explicit:

1. `POST /api/v1/scan` collects cluster state and runs the analyzers.
2. A successful report atomically replaces the latest in-memory report.
3. `GET /api/v1/report/latest` returns that report without contacting Kubernetes.

Failed scans never overwrite the last successful report. This keeps dashboard page loads fast and prevents a read from unexpectedly causing a cluster-wide operation.

## What it checks

- Deployments, StatefulSets, and DaemonSets
- Regular, init, and ephemeral containers
- Host namespace use, privileged execution, privilege escalation, and non-root enforcement
- CPU and memory requests and memory limits
- Versioned upgrade rules embedded into the binary and validated at startup
- Kubernetes 1.35 `StorageVersionMigration` v1alpha1 removal evidence
- Kubernetes 1.36 Service `spec.externalIPs` deprecation

Each signal includes a stable fingerprint, resource and container identity, field path, current and expected values, remediation, and a documentation link.

## Requirements

- Go 1.26.4 or newer
- Node.js 24 and npm for the dashboard
- Access to a Kubernetes cluster through in-cluster credentials or `KUBECONFIG`

## Run locally

Start the API:

```bash
go run ./cmd/api
```

In another terminal, start the dashboard:

```bash
cd web
npm ci
npm run dev
```

Vite proxies `/api` to `http://localhost:8080`, so local development does not require CORS configuration.

Open `http://localhost:5173`. The dashboard first requests the latest stored report; it does not scan until **Scan cluster** is selected.

## API

Run a scan against a supported target:

```bash
curl -X POST 'http://localhost:8080/api/v1/scan?targetVersion=1.36'
```

Read the latest successful report:

```bash
curl 'http://localhost:8080/api/v1/report/latest'
```

Before the first successful scan, the latest endpoint returns `404`. A second scan requested while one is running returns `409`. Invalid targets return `400`; a target that is not newer than the connected cluster returns `422`.

Other endpoint:

```text
GET /api/v1/health
```

Supported target versions are defined by the files in [`rules/kubernetes`](rules/kubernetes). Kubernetes 1.37 is currently a prerelease target and intentionally contains only verified machine-detectable rules.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `KUBEIMPACT_ADDR` | `:8080` | API listen address |
| `KUBEIMPACT_SCAN_TIMEOUT` | `60s` | Maximum duration of a scan |
| `KUBEIMPACT_WEB_DIR` | `/web` | Directory containing the built dashboard |
| `KUBEIMPACT_CORS_ORIGIN` | empty | One exact origin allowed to call the API |
| `KUBECONFIG` | client-go default | Local Kubernetes credentials |
| `VITE_API_BASE_URL` | empty | Dashboard API origin when it is hosted separately |

When the UI and API are served by the same process, leave CORS disabled. Do not expose this API publicly without an authentication and authorization layer: reports contain cluster metadata and scans use cluster-wide read permissions.

## Container image

The image builds both the dashboard and API, embeds the rule files in the Go binary, and runs as a non-root user:

```bash
docker build -t kubeimpact:dev .
docker run --rm -p 8080:8080 \
  --user "$(id -u):$(id -g)" \
  -e KUBECONFIG=/config/kubeconfig \
  -v "$HOME/.kube/config:/config/kubeconfig:ro" \
  kubeimpact:dev
```

Open `http://localhost:8080`.

## Kubernetes deployment

Edit the image in [`deploy/kubernetes.yaml`](deploy/kubernetes.yaml), then apply it:

```bash
kubectl apply -f deploy/kubernetes.yaml
kubectl -n kube-system port-forward service/kubeimpact 8080:80
```

The example deliberately runs one replica because the latest report is process memory. Multiple replicas would each hold a different latest report unless requests were pinned to one instance.

## Rules

Rules are strict YAML: unknown fields, missing required values, invalid severities, mismatched versions, unknown evaluators, and duplicate IDs stop the service at startup.

API rules describe a `groupVersion` and `kind`. Resource checks name an evaluator implemented by the upgrade analyzer. Every rule needs a unique ID, remediation, and an official documentation URL.

Upgrade analysis loads every rule file between the connected cluster's minor version and the requested target. For example, upgrading from 1.34 to 1.36 evaluates both the 1.35 and 1.36 rule sets.

## Scoring

The report starts at 100 and applies severity weights of 25/10/5/2 for critical/high/medium/low signals. Penalties are capped per severity at 50/30/15/5 points so cluster size alone does not make the score unbounded. The API exposes the applied penalties in `scoreBreakdown`; the severity summary includes both current-state findings and upgrade impacts.

Treat the score as prioritization help, not an upgrade guarantee.

## Limitations

- The latest report is held in memory and is lost when the process restarts.
- A single process allows only one scan at a time.
- Removed/deprecated API evidence comes from `metadata.managedFields[].apiVersion`. It can miss manifests or clients that did not leave managed-fields evidence. Verify source manifests, API audit data, and `apiserver_requested_deprecated_apis` before an upgrade.
- KubeImpact currently analyzes live cluster state; scanning Helm charts, Kustomize overlays, and Git repositories is not implemented.
- The included rules intentionally cover only verified, machine-detectable changes. Release-note review is still required.

A natural next storage step is a small persistent repository keyed by scan ID, with scan status/history endpoints and a shared backend for multiple replicas.

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
