import { loadConfig } from "../db";

export function extractWorkId(input: string): string | null {
  const m = input.match(/\/works\/(\d+)/);
  if (m) return m[1];
  const direct = input.match(/^(\d{5,12})$/);
  return direct ? direct[1] : null;
}

export function isAo3Url(input: string): boolean {
  return /archiveofourown\.org/.test(input);
}

async function fetchWith(url: string, cookie: string, userAgent: string): Promise<Response> {
  return fetch(url, {
    headers: {
      "user-agent": userAgent,
      cookie,
      accept: "text/html,application/xhtml+xml,*/*;q=0.8",
      "accept-language": "en-US,en;q=0.8",
    },
    redirect: "follow",
  });
}

export async function fetchDownloadHtml(workId: string): Promise<string> {
  const cfg = await loadConfig();
  const cookie = cfg.ao3.cookie || "view_adults=true;";
  const ua = cfg.ao3.userAgent;

  const workUrl = `https://archiveofourown.org/works/${workId}?view_adult=true&view_full_work=true`;
  const r1 = await fetchWith(workUrl, cookie, ua);
  if (!r1.ok) throw new Error(`AO3 work page ${workId} returned ${r1.status}`);
  const workHtml = await r1.text();

  const dl = workHtml.match(/href="(\/downloads\/[^"]+\.html)"/i);
  if (dl) {
    const downloadUrl = `https://archiveofourown.org${dl[1]}`;
    const r2 = await fetchWith(downloadUrl, cookie, ua);
    if (r2.ok) return await r2.text();
  }
  return workHtml;
}
