import type { Report } from "./types";

const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");

export async function getLatestReport(signal?: AbortSignal): Promise<Report | null> {
  const response = await fetch(`${apiBaseUrl}/api/v1/report/latest`, { signal });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(await responseError(response));
  return response.json() as Promise<Report>;
}

export async function scanCluster(targetVersion: string): Promise<Report> {
  const response = await fetch(
    `${apiBaseUrl}/api/v1/scan?targetVersion=${encodeURIComponent(targetVersion)}`,
    { method: "POST" },
  );
  if (!response.ok) throw new Error(await responseError(response));
  return response.json() as Promise<Report>;
}

async function responseError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // The status-based fallback below remains useful for non-JSON proxy errors.
  }
  return `Request failed with status ${response.status}`;
}
