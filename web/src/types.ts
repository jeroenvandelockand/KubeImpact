export type Severity = "critical" | "high" | "medium" | "low" | "info";

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

export interface Report {
  clusterVersion: string;
  targetVersion: string;
  generatedAt: string;
  score: number;
  scoreBreakdown: ScoreBreakdown;
  summary: Summary;
  warnings: string[];
  findings: Finding[];
  upgradeImpact: UpgradeImpact[];
}
