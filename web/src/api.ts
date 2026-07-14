import type { Report, ScanRecord, ScanRequest } from "./types";

const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");

export async function getLatestReport(signal?: AbortSignal): Promise<Report | null> {
  const response = await fetch(`${apiBaseUrl}/api/v1/report/latest`, { signal });
  if (response.status === 404) return null;
  return decode<Report>(response);
}

export async function createScan(request: ScanRequest): Promise<ScanRecord> {
  const response = await fetch(`${apiBaseUrl}/api/v1/scans`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  return decode<ScanRecord>(response);
}

export async function getScan(id: string): Promise<ScanRecord> {
  const response = await fetch(`${apiBaseUrl}/api/v1/scans/${encodeURIComponent(id)}`);
  return decode<ScanRecord>(response);
}

export async function getReports(signal?: AbortSignal): Promise<ScanRecord[]> {
  const response = await fetch(`${apiBaseUrl}/api/v1/reports?limit=20`, { signal });
  const body = await decode<{ reports: ScanRecord[] }>(response);
  return body.reports;
}

async function decode<T>(response: Response): Promise<T> {
  if (!response.ok) throw new Error(await responseError(response));
  return response.json() as Promise<T>;
}

async function responseError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // Use the status fallback for non-JSON proxy errors.
  }
  return `Request failed with status ${response.status}`;
}
