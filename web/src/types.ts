export type Severity = "critical" | "high" | "medium" | "low" | "info";
export type ChangeStatus = "new" | "unchanged" | "resolved";

export interface Finding {
  id: string;
  fingerprint: string;
  analyzer: string;
  severity: Severity;
  category: string;
  namespace: string;
  kind: string;
  name: string;
  container?: string;
  fieldPath?: string;
  source?: string;
  change?: ChangeStatus;
  currentValue?: string;
  expectedValue?: string;
  message: string;
  recommendation?: string;
  documentationUrl?: string;
}

export interface UpgradeImpact {
  rule: string;
  fingerprint: string;
  severity: Severity;
  category?: string;
  namespace: string;
  kind: string;
  name: string;
  container?: string;
  fieldPath?: string;
  source?: string;
  change?: ChangeStatus;
  currentValue?: string;
  expectedValue?: string;
  message: string;
  recommendation: string;
  documentationUrl?: string;
}

export interface Summary {
  critical: number;
  high: number;
  medium: number;
  low: number;
  info: number;
}

export interface ScoreBreakdown {
  baseScore: number;
  penalty: number;
  penaltyCaps: Summary;
  penaltyApplied: Summary;
}

export type SourceType = "cluster" | "directory" | "helm" | "git";

export interface SourceSpec {
  type: Exclude<SourceType, "cluster">;
  path?: string;
  url?: string;
  ref?: string;
  chartPath?: string;
  releaseName?: string;
  namespace?: string;
  valuesFiles?: string[];
}

export interface SourceResult {
  type: SourceType;
  location: string;
  documents: number;
  resources: number;
  warnings: string[];
}

export interface Suppression {
  ruleId: string;
  namespace: string;
  kind: string;
  name: string;
  container?: string;
  source?: string;
  reason: string;
  fingerprint: string;
}

export interface ResolvedSignal {
  fingerprint: string;
  rule: string;
  type: string;
  severity: Severity;
  namespace: string;
  kind: string;
  name: string;
  container?: string;
  source?: string;
  message: string;
  change: ChangeStatus;
}

export interface ReportComparison {
  previousScanId?: string;
  new: number;
  unchanged: number;
  resolved: number;
  resolvedItems: ResolvedSignal[];
}

export interface Report {
  scanId: string;
  clusterVersion: string;
  targetVersion: string;
  generatedAt: string;
  policyProfile: string;
  policyFingerprint: string;
  score: number;
  scoreBreakdown: ScoreBreakdown;
  summary: Summary;
  warnings: string[];
  sources: SourceResult[];
  suppressions: Suppression[];
  comparison: ReportComparison;
  findings: Finding[];
  upgradeImpact: UpgradeImpact[];
}

export interface ScanRequest {
  targetVersion: string;
  currentVersion?: string;
  includeCluster: boolean;
  sources?: SourceSpec[];
}

export type ScanStatus = "pending" | "running" | "completed" | "failed";

export interface ScanRecord {
  id: string;
  status: ScanStatus;
  request: ScanRequest;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  error?: string;
  report?: Report;
}
