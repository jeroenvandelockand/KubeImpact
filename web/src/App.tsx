import { useEffect, useState } from "react";
import { getLatestReport, scanCluster } from "./api";
import type { Finding, Report, Severity, UpgradeImpact } from "./types";
import "./App.css";

const severityOrder: Severity[] = ["critical", "high", "medium", "low", "info"];

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

function ResourceName({
  namespace,
  kind,
  name,
  container,
}: {
  namespace: string;
  kind: string;
  name: string;
  container?: string;
}) {
  return (
    <div>
      <div className="resource-main">
        {kind}/{name}
      </div>
      <div className="resource-sub">
        {namespace || "cluster-scoped"}
        {container ? ` · container ${container}` : ""}
      </div>
    </div>
  );
}

function Evidence({
  fieldPath,
  currentValue,
  expectedValue,
}: {
  fieldPath?: string;
  currentValue?: string;
  expectedValue?: string;
}) {
  if (!fieldPath && !currentValue && !expectedValue) return <>-</>;
  return (
    <div className="evidence">
      {fieldPath && <code>{fieldPath}</code>}
      {currentValue && <span>Current: {currentValue}</span>}
      {expectedValue && <span>Expected: {expectedValue}</span>}
    </div>
  );
}

export default function App() {
  const [targetVersion, setTargetVersion] = useState("1.36");
  const [data, setData] = useState<Report | null>(null);
  const [initialLoading, setInitialLoading] = useState(true);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void getLatestReport(controller.signal)
      .then((report) => {
        setData(report);
        if (report) setTargetVersion(report.targetVersion);
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

  async function runScan() {
    setScanning(true);
    setError(null);
    try {
      setData(await scanCluster(targetVersion));
    } catch (scanError) {
      setError(scanError instanceof Error ? scanError.message : "Unknown error");
    } finally {
      setScanning(false);
    }
  }

  const totalSignals = data ? data.findings.length + data.upgradeImpact.length : 0;

  return (
    <main className="page">
      <header className="hero">
        <div>
          <div className="eyebrow">Kubernetes upgrade intelligence</div>
          <h1>KubeImpact</h1>
          <p>Run an explicit cluster scan, then review the latest stored result.</p>
        </div>

        <div className="actions">
          <label className="select-label">
            Target version
            <select
              value={targetVersion}
              disabled={scanning}
              onChange={(event) => setTargetVersion(event.target.value)}
            >
              <option value="1.35">1.35</option>
              <option value="1.36">1.36</option>
              <option value="1.37">1.37 preview</option>
            </select>
          </label>

          <button disabled={scanning} onClick={() => void runScan()}>
            {scanning ? "Scanning…" : "Scan cluster"}
          </button>
        </div>
      </header>

      {initialLoading && <div className="state-card">Loading the latest report…</div>}
      {scanning && <div className="state-card">Scanning the cluster. The previous report remains available until this scan succeeds.</div>}
      {error && <div className="error-card">{error}</div>}

      {!initialLoading && !data && (
        <div className="state-card no-report">
          <strong>No report yet</strong>
          <span>Select a target version and run the first cluster scan.</span>
        </div>
      )}

      {data && (
        <>
          {data.warnings.map((warning) => (
            <div className="warning-card" key={warning}>{warning}</div>
          ))}

          <section className="grid overview-grid">
            <div className="card score-card">
              <div className="card-label">Upgrade readiness</div>
              <div
                className="score-ring"
                style={{
                  background: `conic-gradient(var(--accent) ${data.score}%, var(--border) ${data.score}%)`,
                }}
              >
                <div className="score-inner">
                  <span>{data.score}</span>
                  <small>/100</small>
                </div>
              </div>
              <div>
                <div className="risk-label">{riskLabel(data.score)}</div>
                <div className="muted">{data.scoreBreakdown.penalty} capped penalty points</div>
              </div>
            </div>

            <div className="card">
              <div className="card-label">Cluster version</div>
              <div className="metric">{data.clusterVersion}</div>
              <div className="muted">Current Kubernetes version</div>
            </div>

            <div className="card">
              <div className="card-label">Target version</div>
              <div className="metric">{data.targetVersion}</div>
              <div className="muted">Selected upgrade target</div>
            </div>

            <div className="card">
              <div className="card-label">Latest scan</div>
              <div className="metric">{totalSignals}</div>
              <div className="muted">
                {data.findings.length} findings · {data.upgradeImpact.length} upgrade impacts
                <br />{new Date(data.generatedAt).toLocaleString()}
              </div>
            </div>
          </section>

          <section className="grid severity-grid">
            {severityOrder.map((severity) => (
              <div className="card severity-card" key={severity}>
                <SeverityBadge severity={severity} />
                <div className="severity-count">{data.summary[severity]}</div>
              </div>
            ))}
          </section>

          <section className="panel">
            <div className="panel-header">
              <div>
                <h2>Upgrade impact</h2>
                <p>Risks related to upgrading through Kubernetes {data.targetVersion}.</p>
              </div>
              <span className="pill">{data.upgradeImpact.length} items</span>
            </div>
            <UpgradeImpactTable items={data.upgradeImpact} />
          </section>

          <section className="panel">
            <div className="panel-header">
              <div>
                <h2>Findings</h2>
                <p>Current-state workload security and resource findings.</p>
              </div>
              <span className="pill">{data.findings.length} items</span>
            </div>
            <FindingsTable items={data.findings} />
          </section>
        </>
      )}
    </main>
  );
}

function UpgradeImpactTable({ items }: { items: UpgradeImpact[] }) {
  if (items.length === 0) return <div className="empty">No upgrade impact detected.</div>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr><th>Severity</th><th>Rule</th><th>Resource</th><th>Evidence</th><th>Message</th><th>Recommendation</th></tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.fingerprint}>
              <td><SeverityBadge severity={item.severity} /></td>
              <td>{item.rule}</td>
              <td><ResourceName {...item} /></td>
              <td><Evidence {...item} /></td>
              <td>{item.message}</td>
              <td>
                {item.recommendation}
                {item.documentationUrl && <><br /><a href={item.documentationUrl} target="_blank" rel="noreferrer">Documentation</a></>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FindingsTable({ items }: { items: Finding[] }) {
  if (items.length === 0) return <div className="empty">No findings detected.</div>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr><th>Severity</th><th>Rule</th><th>Resource</th><th>Evidence</th><th>Message</th><th>Recommendation</th></tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.fingerprint}>
              <td><SeverityBadge severity={item.severity} /></td>
              <td>{item.id}</td>
              <td><ResourceName {...item} /></td>
              <td><Evidence {...item} /></td>
              <td>{item.message}</td>
              <td>
                {item.recommendation || "-"}
                {item.documentationUrl && <><br /><a href={item.documentationUrl} target="_blank" rel="noreferrer">Documentation</a></>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
