import { spawn } from "node:child_process";
import { cp, mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const WEB = path.join(ROOT, "web");
const WEB_DIST = path.join(WEB, "dist");
const EMBED_DIST = path.join(ROOT, "internal", "webassets", "web-dist");
const OUT_DIR = path.join(ROOT, "server", "build");
const OUT_NAME = process.env.AO3HUB_OUT ?? "ao3-hub";
const OUT_PATH = path.join(OUT_DIR, OUT_NAME);

function goEnvForTarget() {
  const target = process.env.AO3HUB_TARGET?.trim();
  if (!target) return {};
  const [goos, arch] = target.split("-");
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
  await run("npm", ["run", "build", "--workspace", "@ao3hub/web"]);

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
    `-X ao3hub/internal/app.BuiltAt=${process.env.AO3HUB_BUILT_AT ?? new Date().toISOString()}`,
  ].join(" ");

  await run("go", ["build", "-trimpath", `-ldflags=${ldflags}`, "-o", OUT_PATH, "./cmd/ao3hub"], {
    env,
  });

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
  const pkg = JSON.parse(await readFile(path.join(ROOT, "package.json"), "utf8"));
  return String(pkg.version ?? "0.0.0");
}

async function run(command, args, options = {}) {
  await new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: ROOT,
      env: options.env ?? process.env,
      shell: process.platform === "win32",
      stdio: "inherit",
    });
    child.on("error", reject);
    child.on("exit", (code, signal) => {
      if (code === 0) {
        resolve();
        return;
      }
      const suffix = signal ? `signal ${signal}` : `exit code ${code}`;
      reject(new Error(`${command} ${args.join(" ")} failed with ${suffix}`));
    });
  });
}

await main();
