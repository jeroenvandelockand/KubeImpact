# Kubernetes 1.36 to 1.37 upgrade demo

This demo proves KubeImpact behaves as an upgrade tool, not just as a static manifest linter. It creates a digest-pinned Kubernetes 1.36 Kind cluster, builds the repository as `kubeimpact:demo`, mounts an intentionally blocked source tree read-only at `/sources`, and queues a real asynchronous scan targeting Kubernetes 1.37.

## Run it

Requirements are Docker, Kind, kubectl, curl, jq, and make. Check them and build the full demo:

```bash
make doctor
make demo
```

`make demo` leaves the initial report available and prints a compact upgrade-impact table. The fixture covers the source-detectable Kubernetes 1.37 API removals, deprecations, configuration changes, removed flags and gates, and upgrade advisories while the connected cluster supplies the actual current version. The dashboard and API remain available through the managed port-forward at <http://127.0.0.1:18080> until `make destroy` (override the port with `KUBEIMPACT_PORT`).

The request sent to the API is:

```json
{
  "targetVersion": "1.37",
  "includeCluster": true,
  "sources": [{"type": "directory", "path": "upgrade"}]
}
```

The API first returns `202 Accepted` with a pending scan record. The script then polls the scan-specific endpoint with a bounded timeout and saves the completed JSON record under the runtime directory shown by `make status`.

Run the complete proof:

```bash
make test
```

The suite checks:

1. the connected cluster reports Kubernetes 1.36 and the report targets 1.37;
2. the source emits exactly the configured 1.37 rule IDs;
3. remediation replaces the ConfigMap content at the same `/sources/upgrade` path and repeats the byte-for-byte same scan request;
4. the next report has no fixture upgrade impacts and classifies the original impacts as resolved;
5. both reports remain queryable after the KubeImpact pod is replaced, proving that SQLite data lives on the PVC rather than in the pod.

The expected IDs are maintained one per line in `versions.env`. When developing a changed fixture or rule set, override the complete expected set without editing the test logic:

```bash
KUBEIMPACT_EXPECTED_RULE_IDS="RULE-ID-ONE RULE-ID-TWO" make test-upgrade
```

## Explore and operate

```bash
make report          # compact latest saved report
make status          # cluster, fixture phase, API, and latest report
make remediate       # manually apply supported APIs and rescan
make reset-fixture   # restore the blocked source in place
make scan            # scan whichever fixture phase is mounted
make diagnostics     # events, describe output, logs, and saved scan context
make destroy         # delete only the named Kind cluster and marked runtime directory
```

Use `KUBEIMPACT_PORT` to select another local port, `CLUSTER_NAME` to select a safe Kind cluster name, and `KUBEIMPACT_SCAN_WAIT_SECONDS` / `KUBEIMPACT_ROLLOUT_WAIT_SECONDS` to adjust bounded waits.

The fixture is a ConfigMap volume because the demo needs a portable, read-only source mount without binding the repository into the Kind node. KubeImpact analyzes its deliberately synthetic component and kubeadm configuration as source documents and never submits those documents to the Kubernetes API.
