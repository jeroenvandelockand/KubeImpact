# Kubernetes 1.37 upgrade coverage

KubeImpact's 1.37 bundle contains 20 machine-detectable rules. A report is an evidence-backed upgrade assessment, not a guarantee: a clean result means no configured rule matched the evidence supplied to that scan.

## Rules and evidence

| Area | Rule IDs | Evidence |
| --- | --- | --- |
| Removed scheduling APIs | `UPG-1.37-WORKLOAD-V1ALPHA2`, `UPG-1.37-PODGROUP-V1ALPHA2` | Exact source `apiVersion`; live object presence on the removed v1alpha2 endpoint |
| Removed kubeadm API | `UPG-1.37-KUBEADM-INIT-V1BETA3`, `UPG-1.37-KUBEADM-CLUSTER-V1BETA3`, `UPG-1.37-KUBEADM-JOIN-V1BETA3` | Source documents only; kubeadm configuration is not a persisted Kubernetes API resource |
| Deprecated beta APIs | `UPG-1.37-CLUSTERTRUSTBUNDLE-V1BETA1`, `UPG-1.37-PODCERTIFICATEREQUEST-V1BETA1`, `UPG-1.37-STORAGEMIGRATION-V1BETA1` | Exact source `apiVersion`; live managed-fields evidence when available |
| Kubelet changes | `UPG-1.37-KUBELET-EVENTRECORDQPS-ZERO`, `UPG-1.37-CGROUP-V1-OVERRIDE` | `KubeletConfiguration` source fields |
| SELinuxMount | `UPG-1.37-SELINUX-VOLUME-CONFLICT` | Events emitted by the Kubernetes 1.36 `selinux_warning` controller for the three upstream conflict reasons |
| Network and DNS | `UPG-1.37-KUBEPROXY-IPVS`, `UPG-1.37-KUBEPROXY-MODE-UNSET`, `UPG-1.37-KUBEDNS` | `KubeProxyConfiguration`; kube-dns container name or legacy image evidence |
| Authorization review | `UPG-1.37-NODE-LOGS-RBAC` | ClusterRoles granting read access to `nodes/logs` |
| Static Pods | `UPG-1.37-STATIC-POD-API-REFERENCE` | Confirmed mirror Pods, or Pod sources explicitly marked `kubernetes.io/config.source: file|http`, with ServiceAccount, Secret, ConfigMap, PVC, CSI, ResourceClaim, or other API references |
| Cloud controller | `UPG-1.37-CLOUD-NODE-MONITOR-PERIOD` | Old `kubeCloudShared.nodeMonitorPeriod` field in `CloudControllerManagerConfiguration` |
| Feature gates | `UPG-1.37-LOCKED-FEATURE-GATE`, `UPG-1.37-REMOVED-FEATURE-GATE` | Component configuration maps, kubeadm extra arguments, and joined or split component command arguments |
| Removed flags | `UPG-1.37-REMOVED-COMPONENT-FLAG` | Component Pod/workload arguments and kubeadm `extraArgs` / `kubeletExtraArgs` |

Live object reads can be converted by the API server, so KubeImpact does not infer deprecated API use merely because a beta endpoint can return an object. Exact source versions, managed fields, and API request metrics remain distinct evidence channels.

## Evidence that needs another data source

KubeImpact deliberately does not guess at these Kubernetes 1.37 changes:

- `metrics.k8s.io/v1beta1` client usage: NodeMetrics and PodMetrics are read-only aggregated APIs, so inventory is not proof that a client calls the beta endpoint.
- DRAResourceHealth gRPC `v1alpha1`: this is a kubelet-to-plugin protocol rather than a Kubernetes object API.
- SELinux warning metrics: the strongest controller and kubelet metrics are not exposed by the API-server `/metrics` endpoint. Event collection is best effort and reports partial evidence when unavailable.
- Removed cAdvisor metric consumers: dashboards, alerts, and scrapers referencing removed metric families require monitoring-configuration analysis.
- `kubectl run --filename/-f`: detecting it requires scanning shell scripts and CI definitions.
- Node-local or systemd component arguments: the Kubernetes API cannot inventory these files. Supply the original configuration as a directory, Git, or rendered source.
- Component configuration embedded as text inside ConfigMap data: supply the decoded configuration file directly.
- Unannotated Pod YAML is not assumed to be a static Pod because the same manifest can be submitted to the API server as a regular Pod. Preserve `kubernetes.io/config.source: file|http` on static-Pod source fixtures, or scan its live mirror Pod.

The authoritative baseline is the [Kubernetes v1.37.1 changelog](https://github.com/kubernetes/kubernetes/blob/v1.37.1/CHANGELOG/CHANGELOG-1.37.md). The demo asserts that every rule ID in `rules/kubernetes/1.37.yaml` has a blocked fixture and a remediated counterpart.
