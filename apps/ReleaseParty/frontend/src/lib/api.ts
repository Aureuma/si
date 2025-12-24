export type InstallURLResponse = { url: string };

const apiBase = (import.meta as any).env?.PUBLIC_API_BASE || '';

export async function fetchInstallURL(): Promise<string> {
  const res = await fetch(`${apiBase}/api/install/url`);
  if (!res.ok) {
    throw new Error(`Failed to load install URL (${res.status})`);
  }
  const data = (await res.json()) as InstallURLResponse;
  return data.url;
}

