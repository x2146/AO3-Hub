import { $ } from "bun";
import { cp, mkdir, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");
const WEB = path.join(ROOT, "web");
const WEB_DIST = path.join(WEB, "dist");
const EMBED_DIST = path.join(ROOT, "internal", "webassets", "web-dist");
const OUT_DIR = path.join(ROOT, "server", "build");
const OUT_NAME = process.env.AO3HUB_OUT ?? "ao3-hub";
const OUT_PATH = path.join(OUT_DIR, OUT_NAME);

function goEnvForTarget() {
  const target = process.env.AO3HUB_TARGET?.trim();
  if (!target) return {};
  const normalized = target.replace(/^bun-/, "");
  const [goos, arch] = normalized.split("-");
  if (!goos || !arch) {
    throw new Error(`Invalid AO3HUB_TARGET: ${target}`);
  }
  return {
    GOOS: goos === "win32" ? "windows" : goos,
    GOARCH: arch === "x64" ? "amd64" : arch,
  };
}

async function main() {
  console.log("[build] vite build");
  await $`bun run build`.cwd(WEB);

  console.log("[build] embedding web/dist");
  await rm(EMBED_DIST, { recursive: true, force: true });
  await mkdir(path.dirname(EMBED_DIST), { recursive: true });
  await cp(WEB_DIST, EMBED_DIST, { recursive: true });

  console.log(`[build] compiling Go -> ${OUT_PATH}`);
  await mkdir(OUT_DIR, { recursive: true });
  const env = {
    ...process.env,
    ...goEnvForTarget(),
    CGO_ENABLED: process.env.CGO_ENABLED ?? "0",
  };
  const ldflags = [
    `-X ao3hub/internal/app.Version=${process.env.AO3HUB_VERSION ?? (await packageVersion())}`,
    `-X ao3hub/internal/app.BuiltAt=${new Date().toISOString()}`,
  ].join(" ");

  await $`go build -trimpath -ldflags=${ldflags} -o ${OUT_PATH} ./cmd/ao3hub`.env(env);

  console.log("[build] restoring empty embed directory");
  await rm(EMBED_DIST, { recursive: true, force: true });
  await mkdir(EMBED_DIST, { recursive: true });
  await writeFile(
    path.join(EMBED_DIST, "placeholder.txt"),
    "AO3-Hub web assets are generated during build.\n",
  );

  const stats = await stat(OUT_PATH);
  console.log(`[build] done: ${OUT_PATH}  ${(stats.size / 1024 / 1024).toFixed(1)} MB`);
}

async function packageVersion() {
  const pkg = await Bun.file(path.join(ROOT, "package.json")).json();
  return String(pkg.version ?? "0.0.0");
}

await main();
