import { Manifest, type VersionInfo } from "@ao3hub/shared";
import { rename, chmod, unlink } from "node:fs/promises";
import path from "node:path";
import { ARCH, BUILT_AT, PLATFORM, VERSION } from "./env";
import { loadConfig } from "./db";

function compareSemver(a: string, b: string): number {
  const pa = a.replace(/^v/, "").split(".").map((x) => parseInt(x, 10) || 0);
  const pb = b.replace(/^v/, "").split(".").map((x) => parseInt(x, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0;
    const y = pb[i] ?? 0;
    if (x !== y) return x - y;
  }
  return 0;
}

function platformMatch(asset: { platform: string; arch: string }): boolean {
  return asset.platform === PLATFORM && asset.arch === ARCH;
}

export async function fetchManifest(): Promise<{ manifest: Manifest | null; error?: string }> {
  const cfg = await loadConfig();
  const url = cfg.update.manifestURL.trim();
  if (!url) return { manifest: null, error: "未配置 manifest URL" };
  try {
    const res = await fetch(url, { headers: { "cache-control": "no-cache" } });
    if (!res.ok) return { manifest: null, error: `manifest fetch failed: ${res.status}` };
    const json = await res.json();
    const parsed = Manifest.safeParse(json);
    if (!parsed.success) return { manifest: null, error: "manifest schema 校验失败" };
    return { manifest: parsed.data };
  } catch (e) {
    return { manifest: null, error: e instanceof Error ? e.message : String(e) };
  }
}

export async function versionInfo(): Promise<VersionInfo> {
  const base: VersionInfo = {
    current: VERSION,
    platform: PLATFORM,
    arch: ARCH,
    builtAt: BUILT_AT,
  };
  const { manifest } = await fetchManifest();
  if (!manifest) return base;
  const asset = manifest.assets.find(platformMatch);
  const hasUpdate = compareSemver(manifest.version, VERSION) > 0;
  return {
    ...base,
    latest: {
      version: manifest.version,
      notes: manifest.notes,
      publishedAt: manifest.publishedAt,
      hasUpdate,
      downloadUrl: asset?.url,
    },
  };
}

async function sha256Hex(file: string): Promise<string> {
  const buf = await Bun.file(file).arrayBuffer();
  const hash = await crypto.subtle.digest("SHA-256", buf);
  return Array.from(new Uint8Array(hash))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export type ApplyResult = {
  ok: boolean;
  version?: string;
  message: string;
  restart?: boolean;
};

export async function applyUpdate(opts: { force?: boolean } = {}): Promise<ApplyResult> {
  const { manifest, error } = await fetchManifest();
  if (!manifest) return { ok: false, message: error ?? "无法获取 manifest" };
  if (!opts.force && compareSemver(manifest.version, VERSION) <= 0) {
    return { ok: false, message: `当前 ${VERSION} 已是最新（remote ${manifest.version}）` };
  }
  const asset = manifest.assets.find(platformMatch);
  if (!asset) {
    return { ok: false, message: `manifest 中无 ${PLATFORM}/${ARCH} 资源` };
  }

  const exec = process.execPath;
  const dir = path.dirname(exec);
  const tmp = path.join(dir, `.ao3-hub.new-${Date.now()}`);

  const res = await fetch(asset.url);
  if (!res.ok) return { ok: false, message: `下载失败: ${res.status}` };
  const buf = await res.arrayBuffer();
  await Bun.write(tmp, buf);

  if (asset.sha256) {
    const sum = await sha256Hex(tmp);
    if (sum.toLowerCase() !== asset.sha256.toLowerCase()) {
      await unlink(tmp).catch(() => {});
      return { ok: false, message: `sha256 校验失败 expected=${asset.sha256} got=${sum}` };
    }
  }

  await chmod(tmp, 0o755);

  const backup = path.join(dir, `.ao3-hub.bak-${VERSION}`);
  try {
    await rename(exec, backup);
  } catch {}
  await rename(tmp, exec);

  return {
    ok: true,
    version: manifest.version,
    message: `已升级到 ${manifest.version}，进程即将退出，等待 launcher 重启`,
    restart: true,
  };
}

export function scheduleExit(delayMs = 600): void {
  setTimeout(() => process.exit(0), delayMs);
}
