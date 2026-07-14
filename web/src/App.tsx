import { useEffect, useState } from "react";
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

function severityLabel(severity: Severity) {
  return severity.charAt(0).toUpperCase() + severity.slice(1);
}

function riskLabel(score: number) {
  if (score >= 90) return "Low risk";
  if (score >= 70) return "Medium risk";
  return "High risk";
}

function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`badge badge-${severity}`}>{severityLabel(severity)}</span>;
}

function ChangeBadge({ change }: { change?: ChangeStatus }) {
  if (!change) return null;
  return <span className={`change change-${change}`}>{change}</span>;
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
  const [error, setError] = useState<string | null>(null);

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

  function addSource() {
    setSources((current) => [...current, { key: Date.now(), type: "directory", path: "" }]);
  }

  function updateSource(key: number, patch: Partial<EditableSource>) {
    setSources((current) => current.map((source) => (source.key === key ? { ...source, ...patch } : source)));
  }

  function changeSourceType(key: number, type: EditableSource["type"]) {
    setSources((current) => current.map((source) => source.key === key ? { key, type, path: "" } : source));
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
    } catch (scanError) {
      setError(scanError instanceof Error ? scanError.message : "Unknown error");
    } finally {
      setActiveScan(null);
    }
  }

  const scanning = activeScan?.status === "pending" || activeScan?.status === "running";

  return (
    <main className="page">
      <header className="hero">
        <div>
          <div className="eyebrow">Kubernetes upgrade intelligence</div>
          <h1>KubeImpact</h1>
          <p>Compare live cluster evidence with manifests, Helm charts, and Git repositories.</p>
        </div>
      </header>

      <section className="panel scan-panel">
        <div className="panel-header">
          <div>
            <h2>New scan</h2>
            <p>Scans run asynchronously and remain available after service restarts.</p>
          </div>
          <button disabled={scanning} onClick={() => void runScan()}>
            {scanning ? `${activeScan?.status}…` : "Start scan"}
          </button>
        </div>
        <div className="scan-form">
          <label className="select-label">
            Target version
            <select value={targetVersion} disabled={scanning} onChange={(event) => setTargetVersion(event.target.value)}>
              <option value="1.35">1.35</option>
              <option value="1.36">1.36</option>
              <option value="1.37">1.37 preview</option>
            </select>
          </label>
          <label className="check-label">
            <input type="checkbox" checked={includeCluster} disabled={scanning} onChange={(event) => setIncludeCluster(event.target.checked)} />
            Include connected cluster
          </label>
          {!includeCluster && (
            <label className="select-label">
              Current version
              <input value={currentVersion} disabled={scanning} onChange={(event) => setCurrentVersion(event.target.value)} placeholder="1.35" />
            </label>
          )}
          <button className="secondary-button" disabled={scanning} onClick={addSource}>Add source</button>
        </div>

        {sources.map((source) => (
          <div className="source-editor" key={source.key}>
            <label className="select-label">
              Source type
              <select value={source.type} onChange={(event) => changeSourceType(source.key, event.target.value as EditableSource["type"])}>
                <option value="directory">Manifest directory/file</option>
                <option value="helm">Local Helm chart</option>
                <option value="git">Git repository</option>
              </select>
            </label>
            {source.type !== "git" && (
              <label className="select-label grow">
                {source.type === "helm" ? "Chart path" : "Path"}
                <input value={source.path || ""} onChange={(event) => updateSource(source.key, { path: event.target.value })} placeholder={source.type === "helm" ? "charts/my-app" : "manifests/"} />
              </label>
            )}
            {source.type === "git" && (
              <>
                <label className="select-label grow">Repository URL<input value={source.url || ""} onChange={(event) => updateSource(source.key, { url: event.target.value })} placeholder="https://github.com/org/repo.git" /></label>
                <label className="select-label">Ref<input value={source.ref || ""} onChange={(event) => updateSource(source.key, { ref: event.target.value })} placeholder="main" /></label>
                <label className="select-label">Manifest path (optional)<input value={source.path || ""} onChange={(event) => updateSource(source.key, { path: event.target.value, chartPath: event.target.value ? "" : source.chartPath })} placeholder="deploy/production" /></label>
                <label className="select-label">Chart path (optional)<input value={source.chartPath || ""} onChange={(event) => updateSource(source.key, { chartPath: event.target.value, path: event.target.value ? "" : source.path })} placeholder="charts/app" /></label>
              </>
            )}
            {(source.type === "helm" || source.type === "git" && source.chartPath) && (
              <>
                <label className="select-label">Release<input value={source.releaseName || ""} onChange={(event) => updateSource(source.key, { releaseName: event.target.value })} placeholder="kubeimpact-scan" /></label>
                <label className="select-label">Namespace<input value={source.namespace || ""} onChange={(event) => updateSource(source.key, { namespace: event.target.value })} placeholder="default" /></label>
                <label className="select-label">Values files<input value={source.valuesText || ""} onChange={(event) => updateSource(source.key, { valuesText: event.target.value })} placeholder="values.yaml, prod.yaml" /></label>
              </>
            )}
            <button className="danger-button" onClick={() => removeSource(source.key)}>Remove</button>
          </div>
        ))}
      </section>

      {initialLoading && <div className="state-card">Loading persisted reports…</div>}
      {activeScan && <div className="state-card">Scan {activeScan.id.slice(0, 8)} is {activeScan.status}.</div>}
      {error && <div className="error-card">{error}</div>}

      {history.length > 0 && (
        <section className="panel history-panel">
          <div className="panel-header"><div><h2>Report history</h2><p>Select any persisted result.</p></div><span className="pill">{history.length} reports</span></div>
          <div className="history-list">
            {history.map((record) => (
              <button className={data?.scanId === record.id ? "history-item active" : "history-item"} key={record.id} onClick={() => record.report && setData(record.report)}>
                <strong>{record.report?.clusterVersion} → {record.report?.targetVersion}</strong>
                <span>{record.completedAt ? new Date(record.completedAt).toLocaleString() : record.status}</span>
                <span>Score {record.report?.score ?? "-"}</span>
              </button>
            ))}
          </div>
        </section>
      )}

      {!initialLoading && !data && <div className="state-card no-report"><strong>No report yet</strong><span>Configure evidence sources and start the first scan.</span></div>}
      {data && <ReportView data={data} />}
    </main>
  );
}

function ReportView({ data }: { data: Report }) {
  const totalSignals = data.findings.length + data.upgradeImpact.length;
  return (
    <>
      {data.warnings.map((warning) => <div className="warning-card" key={warning}>{warning}</div>)}

      <section className="grid overview-grid">
        <div className="card score-card">
          <div className="card-label">Upgrade readiness</div>
          <div className="score-ring" style={{ background: `conic-gradient(var(--accent) ${data.score}%, var(--border) ${data.score}%)` }}>
            <div className="score-inner"><span>{data.score}</span><small>/100</small></div>
          </div>
          <div><div className="risk-label">{riskLabel(data.score)}</div><div className="muted">{data.scoreBreakdown.penalty} capped penalty points</div></div>
        </div>
        <MetricCard label="Cluster version" value={data.clusterVersion} detail={`Policy: ${data.policyProfile}`} />
        <MetricCard label="Target version" value={data.targetVersion} detail={`${data.sources.length} evidence sources`} />
        <MetricCard label="Signals" value={String(totalSignals)} detail={`${data.findings.length} findings · ${data.upgradeImpact.length} upgrade impacts`} />
      </section>

      <section className="grid comparison-grid">
        <MetricCard label="New" value={String(data.comparison.new)} detail="Introduced since comparable scan" />
        <MetricCard label="Unchanged" value={String(data.comparison.unchanged)} detail="Still present" />
        <MetricCard label="Resolved" value={String(data.comparison.resolved)} detail="No longer detected" />
        <MetricCard label="Suppressions" value={String(data.suppressions.length)} detail="Documented exceptions" />
      </section>

      <section className="grid severity-grid">
        {severityOrder.map((severity) => <div className="card severity-card" key={severity}><SeverityBadge severity={severity} /><div className="severity-count">{data.summary[severity]}</div></div>)}
      </section>

      <section className="panel">
        <div className="panel-header"><div><h2>Evidence sources</h2><p>Where this report obtained its desired-state and runtime evidence.</p></div><span className="pill">{data.sources.length} sources</span></div>
        <div className="source-results">
          {data.sources.map((source, index) => <div className="source-result" key={`${source.type}-${source.location}-${index}`}><strong>{source.type}: {source.location}</strong><span>{source.resources} resources · {source.documents} documents</span>{source.warnings.map((warning) => <small key={warning}>{warning}</small>)}</div>)}
        </div>
      </section>

      <SignalPanel title="Upgrade impact" description={`Risks related to upgrading through Kubernetes ${data.targetVersion}.`} count={data.upgradeImpact.length}>
        <UpgradeImpactTable items={data.upgradeImpact} />
      </SignalPanel>
      <SignalPanel title="Findings" description="Current-state workload security and resource findings." count={data.findings.length}>
        <FindingsTable items={data.findings} />
      </SignalPanel>
      {data.comparison.resolvedItems.length > 0 && <SignalPanel title="Resolved" description="Signals present in the previous comparable scan but absent now." count={data.comparison.resolvedItems.length}><ResolvedTable items={data.comparison.resolvedItems} /></SignalPanel>}
      {data.suppressions.length > 0 && (
        <section className="panel"><div className="panel-header"><div><h2>Audited suppressions</h2><p>Exceptions require an annotation with a reason.</p></div><span className="pill">{data.suppressions.length}</span></div><div className="source-results">{data.suppressions.map((item) => <div className="source-result" key={item.fingerprint}><strong>{item.ruleId} · {item.kind}/{item.name}</strong><span>{item.reason}</span><small>{item.source}</small></div>)}</div></section>
      )}
    </>
  );
}

function MetricCard({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <div className="card"><div className="card-label">{label}</div><div className="metric">{value}</div><div className="muted">{detail}</div></div>;
}

function SignalPanel({ title, description, count, children }: { title: string; description: string; count: number; children: React.ReactNode }) {
  return <section className="panel"><div className="panel-header"><div><h2>{title}</h2><p>{description}</p></div><span className="pill">{count} items</span></div>{children}</section>;
}

function ResourceName({ namespace, kind, name, container, source }: { namespace: string; kind: string; name: string; container?: string; source?: string }) {
  return <div><div className="resource-main">{kind}/{name}</div><div className="resource-sub">{namespace || "cluster-scoped"}{container ? ` · ${container}` : ""}</div>{source && <div className="resource-source">{source}</div>}</div>;
}

function Evidence({ fieldPath, currentValue, expectedValue }: { fieldPath?: string; currentValue?: string; expectedValue?: string }) {
  if (!fieldPath && !currentValue && !expectedValue) return <>-</>;
  return <div className="evidence">{fieldPath && <code>{fieldPath}</code>}{currentValue && <span>Current: {currentValue}</span>}{expectedValue && <span>Expected: {expectedValue}</span>}</div>;
}

function UpgradeImpactTable({ items }: { items: UpgradeImpact[] }) {
  if (items.length === 0) return <div className="empty">No upgrade impact detected.</div>;
  return <div className="table-wrap"><table><thead><tr><th>Severity</th><th>Change</th><th>Rule</th><th>Resource</th><th>Evidence</th><th>Message</th><th>Recommendation</th></tr></thead><tbody>{items.map((item) => <tr key={item.fingerprint}><td><SeverityBadge severity={item.severity} /></td><td><ChangeBadge change={item.change} /></td><td>{item.rule}</td><td><ResourceName {...item} /></td><td><Evidence {...item} /></td><td>{item.message}</td><td>{item.recommendation}{item.documentationUrl && <><br /><a href={item.documentationUrl} target="_blank" rel="noreferrer">Documentation</a></>}</td></tr>)}</tbody></table></div>;
}

function FindingsTable({ items }: { items: Finding[] }) {
  if (items.length === 0) return <div className="empty">No findings detected.</div>;
  return <div className="table-wrap"><table><thead><tr><th>Severity</th><th>Change</th><th>Rule</th><th>Resource</th><th>Evidence</th><th>Message</th><th>Recommendation</th></tr></thead><tbody>{items.map((item) => <tr key={item.fingerprint}><td><SeverityBadge severity={item.severity} /></td><td><ChangeBadge change={item.change} /></td><td>{item.id}</td><td><ResourceName {...item} /></td><td><Evidence {...item} /></td><td>{item.message}</td><td>{item.recommendation || "-"}{item.documentationUrl && <><br /><a href={item.documentationUrl} target="_blank" rel="noreferrer">Documentation</a></>}</td></tr>)}</tbody></table></div>;
}

function ResolvedTable({ items }: { items: ResolvedSignal[] }) {
  return <div className="table-wrap"><table><thead><tr><th>Severity</th><th>Rule</th><th>Resource</th><th>Previous message</th></tr></thead><tbody>{items.map((item) => <tr key={item.fingerprint}><td><SeverityBadge severity={item.severity} /></td><td>{item.rule}</td><td><ResourceName {...item} /></td><td>{item.message}</td></tr>)}</tbody></table></div>;
}
