import { useEffect, useMemo, useState } from "react";
import { createScan, getLatestReport, getReports, getScan } from "./api";
import type {
  ChangeStatus,
  Finding,
  Report,
  ResolvedSignal,
  ScanRecord,
  Severity,
  SourceSpec,
  UpgradeImpact,
} from "./types";
import "./App.css";

const severityOrder: Severity[] = ["critical", "high", "medium", "low", "info"];

type EditableSource = SourceSpec & { key: number; valuesText?: string };
type ReportTab = "overview" | "findings" | "upgrade" | "resolved" | "evidence" | "history";

interface SignalRow {
  key: string;
  type: "Finding" | "Upgrade" | "Resolved";
  rule: string;
  severity: Severity;
  change?: ChangeStatus;
  namespace: string;
  kind: string;
  name: string;
  container?: string;
  source?: string;
  fieldPath?: string;
  currentValue?: string;
  expectedValue?: string;
  message: string;
  recommendation?: string;
  documentationUrl?: string;
}

function severityLabel(severity: Severity) {
  return severity.charAt(0).toUpperCase() + severity.slice(1);
}

function formatDateTime(value?: string) {
  if (!value) return "—";
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function riskLabel(score: number) {
  if (score >= 90) return "Ready";
  if (score >= 70) return "Review required";
  return "Action required";
}

function riskTone(score: number) {
  if (score >= 90) return "success";
  if (score >= 70) return "warning";
  return "danger";
}

function toFindingRow(item: Finding): SignalRow {
  return {
    ...item,
    key: item.fingerprint,
    type: "Finding",
    rule: item.id,
  };
}

function toUpgradeRow(item: UpgradeImpact): SignalRow {
  return {
    ...item,
    key: item.fingerprint,
    type: "Upgrade",
  };
}

function toResolvedRow(item: ResolvedSignal): SignalRow {
  return {
    ...item,
    key: item.fingerprint,
    type: "Resolved",
  };
}

function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`severity-badge severity-${severity}`}><span className="status-dot" />{severityLabel(severity)}</span>;
}

function ChangeBadge({ change }: { change?: ChangeStatus }) {
  if (!change) return <span className="muted">—</span>;
  return <span className={`change-badge change-${change}`}>{change}</span>;
}

function StatusBadge({ status }: { status: ScanRecord["status"] }) {
  return <span className={`status-badge status-${status}`}><span className="status-dot" />{status}</span>;
}

export default function App() {
  const [targetVersion, setTargetVersion] = useState("1.36");
  const [includeCluster, setIncludeCluster] = useState(true);
  const [currentVersion, setCurrentVersion] = useState("1.35");
  const [sources, setSources] = useState<EditableSource[]>([]);
  const [data, setData] = useState<Report | null>(null);
  const [history, setHistory] = useState<ScanRecord[]>([]);
  const [initialLoading, setInitialLoading] = useState(true);
  const [activeScan, setActiveScan] = useState<ScanRecord | null>(null);
  const [activeTab, setActiveTab] = useState<ReportTab>("overview");
  const [scanOpen, setScanOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scanning = activeScan?.status === "pending" || activeScan?.status === "running";

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([getLatestReport(controller.signal), getReports(controller.signal)])
      .then(([latest, reports]) => {
        setData(latest);
        setHistory(reports);
        if (latest) setTargetVersion(latest.targetVersion);
      })
      .catch((requestError: unknown) => {
        if (requestError instanceof DOMException && requestError.name === "AbortError") return;
        setError(requestError instanceof Error ? requestError.message : "Unknown error");
      })
      .finally(() => {
        if (!controller.signal.aborted) setInitialLoading(false);
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!scanOpen) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape" && !scanning) setScanOpen(false);
    }
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [scanOpen, scanning]);

  function addSource(type: EditableSource["type"] = "directory") {
    setSources((current) => {
      const source: EditableSource = {
        key: Date.now() + current.length,
        type,
        path: "",
      };
      if (type === "helm") {
        source.releaseName = "kubeimpact";
        source.namespace = "default";
      }
      if (type === "git") {
        source.url = "";
        source.ref = "main";
      }
      return [...current, source];
    });
  }

  function updateSource(key: number, patch: Partial<EditableSource>) {
    setSources((current) => current.map((source) => (source.key === key ? { ...source, ...patch } : source)));
  }

  function changeSourceType(key: number, type: EditableSource["type"]) {
    setSources((current) =>
      current.map((source) => {
        if (source.key !== key) return source;
        const nextSource: EditableSource = { key, type, path: "" };
        if (type === "helm") {
          nextSource.releaseName = "kubeimpact";
          nextSource.namespace = "default";
        }
        if (type === "git") {
          nextSource.url = "";
          nextSource.ref = "main";
        }
        return nextSource;
      }),
    );
  }

  function removeSource(key: number) {
    setSources((current) => current.filter((source) => source.key !== key));
  }

  async function runScan() {
    setError(null);
    try {
      const sourceSpecs: SourceSpec[] = sources.map((source) => {
        const { key, valuesText, ...sourceSpec } = source;
        void key;
        return {
          ...sourceSpec,
          valuesFiles: valuesText?.split(",").map((value) => value.trim()).filter(Boolean),
        };
      });
      let record = await createScan({
        targetVersion,
        currentVersion: includeCluster ? undefined : currentVersion,
        includeCluster,
        sources: sourceSpecs,
      });
      setActiveScan(record);
      while (record.status === "pending" || record.status === "running") {
        await new Promise((resolve) => window.setTimeout(resolve, 750));
        record = await getScan(record.id);
        setActiveScan(record);
      }
      if (record.status === "failed") throw new Error(record.error || "Scan failed");
      if (!record.report) throw new Error("Completed scan has no report");
      setData(record.report);
      setHistory(await getReports());
      setActiveTab("overview");
      setScanOpen(false);
    } catch (scanError) {
      setError(scanError instanceof Error ? scanError.message : "Unknown error");
    } finally {
      setActiveScan(null);
    }
  }

  function openReport(report: Report) {
    setData(report);
    setActiveTab("overview");
  }

  function openScanConfig() {
    setError(null);
    setScanOpen(true);
  }

  const tabs: Array<{ id: ReportTab; label: string; count?: number }> = data ? [
    { id: "overview", label: "Overview" },
    { id: "findings", label: "Findings", count: data.findings.length },
    { id: "upgrade", label: "Upgrade impact", count: data.upgradeImpact.length },
    ...(data.comparison.resolvedItems.length > 0 ? [{ id: "resolved" as const, label: "Resolved", count: data.comparison.resolvedItems.length }] : []),
    { id: "evidence", label: "Evidence", count: data.sources.length },
    { id: "history", label: "History", count: history.length },
  ] : [];

  return (
    <div className="app-shell">
      <header className="app-header">
        <div className="brand">
          <div className="brand-mark" aria-hidden="true">K</div>
          <div>
            <div className="brand-name">KubeImpact</div>
            <div className="brand-subtitle">Kubernetes upgrade intelligence</div>
          </div>
        </div>
        <div className="header-actions">
          {data && <div className="last-updated">Last report <strong>{formatDateTime(data.generatedAt)}</strong></div>}
          <button type="button" className="primary-button" disabled={scanning} onClick={openScanConfig}>
            <span aria-hidden="true">＋</span>{scanning ? "Scan running" : "Run new scan"}
          </button>
        </div>
      </header>

      {data && (
        <>
          <section className="report-context">
            <div>
              <div className="breadcrumb">Reports <span>/</span> {data.scanId.slice(0, 8)}</div>
              <div className="report-title-row">
                <h1>Kubernetes {data.clusterVersion} to {data.targetVersion}</h1>
                <span className={`readiness-label tone-${riskTone(data.score)}`}><span className="status-dot" />{riskLabel(data.score)}</span>
              </div>
              <p>Policy profile: {data.policyProfile} · Generated {formatDateTime(data.generatedAt)}</p>
            </div>
            <div className={`context-score score-${riskTone(data.score)}`}>
              <div className="context-score-label"><span>Readiness score</span><span className="status-dot" /></div>
              <strong>{data.score}<small>/100</small></strong>
              <div className="score-track" aria-hidden="true"><span style={{ width: `${data.score}%` }} /></div>
            </div>
          </section>

          <nav className="report-tabs" aria-label="Report sections">
            {tabs.map((tab) => (
              <button
                type="button"
                key={tab.id}
                className={activeTab === tab.id ? "active" : ""}
                aria-current={activeTab === tab.id ? "page" : undefined}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}{tab.count !== undefined && <span>{tab.count}</span>}
              </button>
            ))}
          </nav>
        </>
      )}

      <main className="workspace">
        {activeScan && (
          <div className="notice notice-info">
            <span className="spinner" aria-hidden="true" />
            Scan {activeScan.id.slice(0, 8)} is {activeScan.status}. Results will appear automatically.
          </div>
        )}
        {error && <div className="notice notice-danger"><strong>Request failed.</strong> {error}</div>}
        {data?.warnings.map((warning) => <div className="notice notice-warning" key={warning}>{warning}</div>)}

        {initialLoading && <LoadingState />}
        {!initialLoading && !data && <EmptyDashboard onStart={openScanConfig} />}
        {data && (
          <ReportView
            key={data.scanId}
            data={data}
            history={history}
            activeTab={activeTab}
            onTabChange={setActiveTab}
            onOpenReport={openReport}
          />
        )}
      </main>

      {scanOpen && (
        <div className="drawer-backdrop" onMouseDown={(event) => {
          if (event.target === event.currentTarget && !scanning) setScanOpen(false);
        }}>
          <aside className="drawer scan-drawer" role="dialog" aria-modal="true" aria-labelledby="scan-title">
            <div className="drawer-header">
              <div>
                <h2 id="scan-title">Run a new scan</h2>
                <p>Choose an upgrade target and the evidence to inspect.</p>
              </div>
              <button type="button" className="icon-button" aria-label="Close scan configuration" disabled={scanning} onClick={() => setScanOpen(false)}>×</button>
            </div>

            <div className="drawer-body">
              {error && <div className="notice notice-danger drawer-notice"><strong>Scan failed.</strong> {error}</div>}
              <section className="form-section">
                <div className="section-heading">
                  <span className="step-number">1</span>
                  <div><h3>Upgrade target</h3><p>Define the Kubernetes versions in scope.</p></div>
                </div>
                <div className="form-row">
                  {!includeCluster && (
                    <label className="field-label">
                      Current version
                      <input value={currentVersion} disabled={scanning} onChange={(event) => setCurrentVersion(event.target.value)} placeholder="1.35" />
                    </label>
                  )}
                  <label className="field-label">
                    Target version
                    <select value={targetVersion} disabled={scanning} onChange={(event) => setTargetVersion(event.target.value)}>
                      <option value="1.35">1.35</option>
                      <option value="1.36">1.36</option>
                      <option value="1.37">1.37 preview</option>
                    </select>
                  </label>
                </div>
                <label className="toggle-row">
                  <div><strong>Include connected cluster</strong><span>Collect live objects and runtime API usage.</span></div>
                  <input type="checkbox" checked={includeCluster} disabled={scanning} onChange={(event) => setIncludeCluster(event.target.checked)} />
                  <span className="toggle" aria-hidden="true"><span /></span>
                </label>
              </section>

              <section className="form-section">
                <div className="section-heading source-heading">
                  <span className="step-number">2</span>
                  <div><h3>Evidence sources</h3><p>Add desired-state sources, or run a cluster-only scan.</p></div>
                </div>
                <div className="add-source-actions">
                  <button type="button" className="secondary-button" disabled={scanning} onClick={() => addSource("directory")}>＋ Manifest</button>
                  <button type="button" className="secondary-button" disabled={scanning} onClick={() => addSource("helm")}>＋ Helm</button>
                  <button type="button" className="secondary-button" disabled={scanning} onClick={() => addSource("git")}>＋ Git</button>
                </div>

                {sources.length === 0 && (
                  <div className="source-empty">
                    <strong>No additional sources</strong>
                    <span>{includeCluster ? "This scan will use connected-cluster evidence only." : "Add a source before running a manifest-only scan."}</span>
                  </div>
                )}

                <div className="source-editor-list">
                  {sources.map((source, index) => (
                    <SourceEditor
                      key={source.key}
                      source={source}
                      index={index}
                      disabled={Boolean(scanning)}
                      onChange={(patch) => updateSource(source.key, patch)}
                      onTypeChange={(type) => changeSourceType(source.key, type)}
                      onRemove={() => removeSource(source.key)}
                    />
                  ))}
                </div>
              </section>

              <section className="form-section review-section">
                <div className="section-heading">
                  <span className="step-number">3</span>
                  <div><h3>Review</h3><p>{includeCluster ? "Connected cluster" : `Kubernetes ${currentVersion}`} → Kubernetes {targetVersion} · {sources.length} configured source{sources.length === 1 ? "" : "s"}</p></div>
                </div>
              </section>
            </div>

            <div className="drawer-footer">
              <button type="button" className="tertiary-button" disabled={scanning} onClick={() => setScanOpen(false)}>Cancel</button>
              <button type="button" className="primary-button" disabled={Boolean(scanning) || (!includeCluster && sources.length === 0)} onClick={() => void runScan()}>
                {scanning ? <><span className="spinner spinner-light" />{activeScan?.status}…</> : "Run scan"}
              </button>
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}

function LoadingState() {
  return (
    <div className="loading-state">
      <span className="spinner" aria-hidden="true" />
      <div><strong>Loading dashboard</strong><span>Retrieving persisted reports…</span></div>
    </div>
  );
}

function EmptyDashboard({ onStart }: { onStart: () => void }) {
  return (
    <section className="empty-dashboard">
      <div className="empty-icon" aria-hidden="true">K</div>
      <h1>Assess your next Kubernetes upgrade</h1>
      <p>Combine cluster activity with manifests, Helm charts, and Git repositories to find upgrade blockers before rollout.</p>
      <button type="button" className="primary-button" onClick={onStart}>Run your first scan</button>
      <div className="empty-capabilities">
        <div><strong>Live cluster</strong><span>Workloads and deprecated API requests</span></div>
        <div><strong>Declared state</strong><span>Raw YAML and Git-hosted manifests</span></div>
        <div><strong>Rendered state</strong><span>Helm output with selected values</span></div>
      </div>
    </section>
  );
}

function SourceEditor({
  source,
  index,
  disabled,
  onChange,
  onTypeChange,
  onRemove,
}: {
  source: EditableSource;
  index: number;
  disabled: boolean;
  onChange: (patch: Partial<EditableSource>) => void;
  onTypeChange: (type: EditableSource["type"]) => void;
  onRemove: () => void;
}) {
  return (
    <div className="source-editor">
      <div className="source-editor-header">
        <div><strong>Source {index + 1}</strong><span>{source.type === "directory" ? "Manifest file or directory" : source.type === "helm" ? "Local Helm chart" : "Remote Git repository"}</span></div>
        <button type="button" className="text-button danger-text" disabled={disabled} onClick={onRemove}>Remove</button>
      </div>
      <label className="field-label">
        Source type
        <select value={source.type} disabled={disabled} onChange={(event) => onTypeChange(event.target.value as EditableSource["type"])}>
          <option value="directory">Manifest directory/file</option>
          <option value="helm">Local Helm chart</option>
          <option value="git">Git repository</option>
        </select>
      </label>
      {source.type !== "git" && (
        <label className="field-label">
          {source.type === "helm" ? "Chart path" : "Path"}
          <input value={source.path || ""} disabled={disabled} onChange={(event) => onChange({ path: event.target.value })} placeholder={source.type === "helm" ? "charts/my-app" : "manifests/"} />
        </label>
      )}
      {source.type === "git" && (
        <>
          <label className="field-label">
            Repository URL
            <input value={source.url || ""} disabled={disabled} onChange={(event) => onChange({ url: event.target.value })} placeholder="https://github.com/org/repo.git" />
          </label>
          <div className="form-row compact-row">
            <label className="field-label">
              Ref
              <input value={source.ref || ""} disabled={disabled} onChange={(event) => onChange({ ref: event.target.value })} placeholder="main" />
            </label>
            <label className="field-label">
              Manifest path
              <input value={source.path || ""} disabled={disabled} onChange={(event) => onChange({ path: event.target.value, chartPath: event.target.value ? "" : source.chartPath })} placeholder="deploy/production" />
            </label>
          </div>
          <label className="field-label">
            Chart path (optional)
            <input value={source.chartPath || ""} disabled={disabled} onChange={(event) => onChange({ chartPath: event.target.value, path: event.target.value ? "" : source.path })} placeholder="charts/app" />
          </label>
        </>
      )}
      {(source.type === "helm" || (source.type === "git" && source.chartPath)) && (
        <>
          <div className="form-row compact-row">
            <label className="field-label">
              Release
              <input value={source.releaseName || ""} disabled={disabled} onChange={(event) => onChange({ releaseName: event.target.value })} placeholder="kubeimpact-scan" />
            </label>
            <label className="field-label">
              Namespace
              <input value={source.namespace || ""} disabled={disabled} onChange={(event) => onChange({ namespace: event.target.value })} placeholder="default" />
            </label>
          </div>
          <label className="field-label">
            Values files
            <input value={source.valuesText || ""} disabled={disabled} onChange={(event) => onChange({ valuesText: event.target.value })} placeholder="values.yaml, prod.yaml" />
          </label>
        </>
      )}
    </div>
  );
}

function ReportView({
  data,
  history,
  activeTab,
  onTabChange,
  onOpenReport,
}: {
  data: Report;
  history: ScanRecord[];
  activeTab: ReportTab;
  onTabChange: (tab: ReportTab) => void;
  onOpenReport: (report: Report) => void;
}) {
  const findingRows = useMemo(() => data.findings.map(toFindingRow), [data.findings]);
  const upgradeRows = useMemo(() => data.upgradeImpact.map(toUpgradeRow), [data.upgradeImpact]);
  const resolvedRows = useMemo(() => data.comparison.resolvedItems.map(toResolvedRow), [data.comparison.resolvedItems]);

  if (activeTab === "findings") {
    return <SignalSection title="Findings" description="Current workload security and resource configuration risks." rows={findingRows} />;
  }
  if (activeTab === "upgrade") {
    return <SignalSection title="Upgrade impact" description={`Compatibility risks on the path to Kubernetes ${data.targetVersion}.`} rows={upgradeRows} />;
  }
  if (activeTab === "resolved") {
    return <SignalSection title="Resolved signals" description="Signals from the previous comparable scan that are no longer detected." rows={resolvedRows} />;
  }
  if (activeTab === "evidence") return <EvidenceView data={data} />;
  if (activeTab === "history") return <HistoryView history={history} selectedScanId={data.scanId} onOpenReport={onOpenReport} />;
  return <Overview data={data} history={history} onTabChange={onTabChange} onOpenReport={onOpenReport} />;
}

function Overview({ data, history, onTabChange, onOpenReport }: { data: Report; history: ScanRecord[]; onTabChange: (tab: ReportTab) => void; onOpenReport: (report: Report) => void }) {
  const signals = useMemo(() => [
    ...data.upgradeImpact.map(toUpgradeRow),
    ...data.findings.map(toFindingRow),
  ], [data.findings, data.upgradeImpact]);
  const totalSignals = signals.length;
  const highPriority = signals.filter((item) => item.severity === "critical" || item.severity === "high").length;
  const resources = data.sources.reduce((total, source) => total + source.resources, 0);
  const topBlockers = [...signals]
    .sort((left, right) => severityOrder.indexOf(left.severity) - severityOrder.indexOf(right.severity))
    .slice(0, 5);
  const severityCounts = severityOrder.reduce<Record<Severity, number>>((counts, severity) => {
    counts[severity] = signals.filter((item) => item.severity === severity).length;
    return counts;
  }, { critical: 0, high: 0, medium: 0, low: 0, info: 0 });
  const chartBackground = severityChart(severityCounts, totalSignals);

  return (
    <div className="overview-stack">
      <section className="metric-grid" aria-label="Report summary">
        <MetricCard label="Readiness" value={`${data.score}/100`} detail={riskLabel(data.score)} tone={riskTone(data.score)} />
        <MetricCard label="Critical & high" value={String(highPriority)} detail={highPriority === 0 ? "No priority blockers" : "Require attention"} tone={highPriority === 0 ? "success" : "danger"} />
        <MetricCard label="Upgrade path" value={`${data.clusterVersion} → ${data.targetVersion}`} detail={`Policy: ${data.policyProfile}`} />
        <MetricCard label="Evidence coverage" value={`${data.sources.length} source${data.sources.length === 1 ? "" : "s"}`} detail={`${resources} resources inspected`} />
      </section>

      <div className="overview-grid">
        <section className="panel severity-panel">
          <PanelHeader title="Signal severity" description="All findings and upgrade-impact signals." />
          <div className="severity-content">
            <div className="donut" style={{ background: chartBackground }} aria-label={`${totalSignals} total signals`}>
              <div><strong>{totalSignals}</strong><span>Total signals</span></div>
            </div>
            <div className="severity-legend">
              {severityOrder.map((severity) => (
                <div key={severity}>
                  <span className={`legend-swatch severity-bg-${severity}`} />
                  <span>{severityLabel(severity)}</span>
                  <strong>{severityCounts[severity]}</strong>
                </div>
              ))}
            </div>
          </div>
          <div className="panel-link-row">
            <button type="button" className="text-button" onClick={() => onTabChange("findings")}>View all findings <span aria-hidden="true">→</span></button>
          </div>
        </section>

        <section className="panel blockers-panel">
          <PanelHeader title="Top blockers" description="Highest-severity signals requiring review." action={<button type="button" className="text-button" onClick={() => onTabChange("upgrade")}>View upgrade impact →</button>} />
          {topBlockers.length === 0 ? (
            <div className="panel-empty success-empty"><span className="success-icon">✓</span><strong>No blockers detected</strong><p>The current evidence did not produce findings or upgrade-impact signals.</p></div>
          ) : (
            <div className="table-wrap">
              <table className="compact-table">
                <thead><tr><th>Severity</th><th>Type</th><th>Resource</th><th>Rule</th><th>Change</th></tr></thead>
                <tbody>
                  {topBlockers.map((item) => (
                    <tr key={`${item.type}-${item.key}`}>
                      <td><SeverityBadge severity={item.severity} /></td>
                      <td>{item.type}</td>
                      <td><ResourceName item={item} /></td>
                      <td><code className="rule-code">{item.rule}</code></td>
                      <td><ChangeBadge change={item.change} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>

      <section className="panel changes-panel">
        <PanelHeader title="Change since previous comparable scan" description={data.comparison.previousScanId ? `Compared with scan ${data.comparison.previousScanId.slice(0, 8)}.` : "No comparable previous scan is available."} />
        <div className="change-summary">
          <div><span className="change-icon change-new-icon">＋</span><strong>{data.comparison.new}</strong><span>New</span></div>
          <div><span className="change-icon change-unchanged-icon">—</span><strong>{data.comparison.unchanged}</strong><span>Unchanged</span></div>
          <div><span className="change-icon change-resolved-icon">✓</span><strong>{data.comparison.resolved}</strong><span>Resolved</span></div>
          <div><span className="change-icon change-suppressed-icon">○</span><strong>{data.suppressions.length}</strong><span>Suppressed</span></div>
        </div>
      </section>

      <RecentScans history={history.slice(0, 5)} selectedScanId={data.scanId} onOpenReport={onOpenReport} onViewAll={() => onTabChange("history")} />
    </div>
  );
}

function MetricCard({ label, value, detail, tone = "neutral" }: { label: string; value: string; detail: string; tone?: string }) {
  return (
    <div className="metric-card">
      <div className="metric-card-label">{label}</div>
      <div className="metric-card-value"><span className={`metric-indicator tone-${tone}`} />{value}</div>
      <div className="metric-card-detail">{detail}</div>
    </div>
  );
}

function PanelHeader({ title, description, action }: { title: string; description: string; action?: React.ReactNode }) {
  return (
    <div className="panel-header">
      <div><h2>{title}</h2><p>{description}</p></div>
      {action}
    </div>
  );
}

function severityChart(counts: Record<Severity, number>, total: number) {
  if (total === 0) return "var(--border-subtle)";
  const colors: Record<Severity, string> = {
    critical: "var(--critical)",
    high: "var(--high)",
    medium: "var(--medium)",
    low: "var(--low)",
    info: "var(--info)",
  };
  let offset = 0;
  const stops = severityOrder.map((severity) => {
    const start = offset;
    offset += (counts[severity] / total) * 100;
    return `${colors[severity]} ${start}% ${offset}%`;
  });
  return `conic-gradient(${stops.join(", ")})`;
}

function SignalSection({ title, description, rows }: { title: string; description: string; rows: SignalRow[] }) {
  const [search, setSearch] = useState("");
  const [severity, setSeverity] = useState<Severity | "all">("all");
  const [change, setChange] = useState<ChangeStatus | "all">("all");
  const [selected, setSelected] = useState<SignalRow | null>(null);
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (severity !== "all" && row.severity !== severity) return false;
      if (change !== "all" && row.change !== change) return false;
      if (!query) return true;
      return [row.rule, row.namespace, row.kind, row.name, row.container, row.source, row.message]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query));
    });
  }, [change, rows, search, severity]);

  return (
    <>
      <section className="panel signal-panel">
        <PanelHeader title={title} description={description} />
        <div className="filter-bar">
          <label className="search-field">
            <span aria-hidden="true">⌕</span>
            <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search resources, rules, or sources" aria-label={`Search ${title.toLowerCase()}`} />
          </label>
          <label className="filter-field">
            <span>Severity</span>
            <select value={severity} onChange={(event) => setSeverity(event.target.value as Severity | "all")}>
              <option value="all">All severities</option>
              {severityOrder.map((item) => <option value={item} key={item}>{severityLabel(item)}</option>)}
            </select>
          </label>
          <label className="filter-field">
            <span>Change</span>
            <select value={change} onChange={(event) => setChange(event.target.value as ChangeStatus | "all")}>
              <option value="all">All changes</option>
              <option value="new">New</option>
              <option value="unchanged">Unchanged</option>
              <option value="resolved">Resolved</option>
            </select>
          </label>
          <div className="result-count">{filtered.length} of {rows.length}</div>
        </div>

        {filtered.length === 0 ? (
          <div className="panel-empty"><strong>No matching signals</strong><p>Adjust the search or filters to see more results.</p></div>
        ) : (
          <div className="table-wrap">
            <table className="signal-table">
              <thead><tr><th>Severity</th><th>Resource</th><th>Rule</th><th>Source</th><th>Change</th><th>Summary</th><th><span className="sr-only">Actions</span></th></tr></thead>
              <tbody>
                {filtered.map((item) => (
                  <tr key={item.key}>
                    <td><SeverityBadge severity={item.severity} /></td>
                    <td><ResourceName item={item} /></td>
                    <td><code className="rule-code">{item.rule}</code></td>
                    <td><div className="source-cell" title={item.source}>{item.source || "—"}</div></td>
                    <td><ChangeBadge change={item.change} /></td>
                    <td><div className="message-cell">{item.message}</div></td>
                    <td><button type="button" className="text-button nowrap" onClick={() => setSelected(item)}>View details</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {selected && <SignalDetail item={selected} onClose={() => setSelected(null)} />}
    </>
  );
}

function SignalDetail({ item, onClose }: { item: SignalRow; onClose: () => void }) {
  return (
    <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <aside className="drawer detail-drawer" role="dialog" aria-modal="true" aria-labelledby="detail-title">
        <div className="drawer-header">
          <div><div className="drawer-kicker">{item.type}</div><h2 id="detail-title">{item.rule}</h2></div>
          <button type="button" className="icon-button" aria-label="Close signal details" onClick={onClose}>×</button>
        </div>
        <div className="drawer-body detail-body">
          <div className="detail-badges"><SeverityBadge severity={item.severity} /><ChangeBadge change={item.change} /></div>
          <section className="detail-section"><h3>Resource</h3><ResourceName item={item} />{item.source && <div className="detail-source">{item.source}</div>}</section>
          <section className="detail-section"><h3>Finding</h3><p>{item.message}</p></section>
          {(item.fieldPath || item.currentValue || item.expectedValue) && (
            <section className="detail-section">
              <h3>Evidence</h3>
              <dl className="detail-list">
                {item.fieldPath && <><dt>Field</dt><dd><code>{item.fieldPath}</code></dd></>}
                {item.currentValue && <><dt>Current</dt><dd>{item.currentValue}</dd></>}
                {item.expectedValue && <><dt>Expected</dt><dd>{item.expectedValue}</dd></>}
              </dl>
            </section>
          )}
          {item.recommendation && <section className="detail-section"><h3>Recommendation</h3><p>{item.recommendation}</p></section>}
          {item.documentationUrl && <a className="documentation-link" href={item.documentationUrl} target="_blank" rel="noreferrer">Open documentation <span aria-hidden="true">↗</span></a>}
        </div>
      </aside>
    </div>
  );
}

function ResourceName({ item }: { item: Pick<SignalRow, "namespace" | "kind" | "name" | "container"> }) {
  return (
    <div className="resource-name">
      <strong>{item.kind}/{item.name}</strong>
      <span>{item.namespace || "cluster-scoped"}{item.container ? ` · ${item.container}` : ""}</span>
    </div>
  );
}

function EvidenceView({ data }: { data: Report }) {
  return (
    <div className="evidence-stack">
      <section className="panel">
        <PanelHeader title="Evidence sources" description="Declared, rendered, and runtime evidence used for this report." />
        <div className="table-wrap">
          <table>
            <thead><tr><th>Type</th><th>Location</th><th>Resources</th><th>Documents</th><th>Status</th></tr></thead>
            <tbody>
              {data.sources.map((source, index) => (
                <tr key={`${source.type}-${source.location}-${index}`}>
                  <td><span className="source-type">{source.type}</span></td>
                  <td><div className="location-cell">{source.location}</div></td>
                  <td>{source.resources}</td>
                  <td>{source.documents}</td>
                  <td>
                    {source.warnings.length === 0 ? (
                      <span className="inline-status tone-success"><span className="status-dot" />Collected</span>
                    ) : (
                      <details className="source-warnings">
                        <summary className="inline-status tone-warning"><span className="status-dot" />{source.warnings.length} warning{source.warnings.length === 1 ? "" : "s"}</summary>
                        <ul>{source.warnings.map((warning, warningIndex) => <li key={`${warningIndex}-${warning}`}>{warning}</li>)}</ul>
                      </details>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="panel">
        <PanelHeader title="Audited suppressions" description="Policy exceptions recorded with an explicit reason." action={<span className="count-pill">{data.suppressions.length}</span>} />
        {data.suppressions.length === 0 ? (
          <div className="panel-empty compact-empty"><strong>No suppressions</strong><p>No findings were excluded from this report.</p></div>
        ) : (
          <div className="suppression-list">
            {data.suppressions.map((item) => (
              <div className="suppression-item" key={item.fingerprint}>
                <div><code className="rule-code">{item.ruleId}</code><strong>{item.kind}/{item.name}</strong><span>{item.namespace || "cluster-scoped"}</span></div>
                <p>{item.reason}</p>
                <small>{item.source || "No source recorded"}</small>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function RecentScans({ history, selectedScanId, onOpenReport, onViewAll }: { history: ScanRecord[]; selectedScanId: string; onOpenReport: (report: Report) => void; onViewAll: () => void }) {
  return (
    <section className="panel">
      <PanelHeader title="Recent scans" description="Latest persisted scan activity." action={<button type="button" className="text-button" onClick={onViewAll}>View history →</button>} />
      <HistoryTable history={history} selectedScanId={selectedScanId} onOpenReport={onOpenReport} />
    </section>
  );
}

function HistoryView({ history, selectedScanId, onOpenReport }: { history: ScanRecord[]; selectedScanId: string; onOpenReport: (report: Report) => void }) {
  return (
    <section className="panel">
      <PanelHeader title="Report history" description="Completed and failed scans persisted in SQLite." action={<span className="count-pill">{history.length} scans</span>} />
      <HistoryTable history={history} selectedScanId={selectedScanId} onOpenReport={onOpenReport} />
    </section>
  );
}

function HistoryTable({ history, selectedScanId, onOpenReport }: { history: ScanRecord[]; selectedScanId: string; onOpenReport: (report: Report) => void }) {
  if (history.length === 0) return <div className="panel-empty"><strong>No scan history</strong><p>Completed scans will appear here.</p></div>;
  return (
    <div className="table-wrap">
      <table className="history-table">
        <thead><tr><th>Completed</th><th>Upgrade path</th><th>Status</th><th>Score</th><th>Signals</th><th>Sources</th><th><span className="sr-only">Action</span></th></tr></thead>
        <tbody>
          {history.map((record) => {
            const report = record.report;
            const selected = report?.scanId === selectedScanId;
            return (
              <tr key={record.id} className={selected ? "selected-row" : ""}>
                <td><strong>{formatDateTime(record.completedAt || record.createdAt)}</strong><span className="table-subtitle">{record.id.slice(0, 8)}</span></td>
                <td>{report ? `${report.clusterVersion} → ${report.targetVersion}` : `${record.request.currentVersion || "Cluster"} → ${record.request.targetVersion}`}</td>
                <td><StatusBadge status={record.status} /></td>
                <td>{report ? <strong>{report.score}/100</strong> : "—"}</td>
                <td>{report ? report.findings.length + report.upgradeImpact.length : "—"}</td>
                <td>{report?.sources.length ?? record.request.sources?.length ?? 0}</td>
                <td>{report && <button type="button" className="text-button nowrap" disabled={selected} onClick={() => onOpenReport(report)}>{selected ? "Current" : "Open report"}</button>}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
