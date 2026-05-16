import { mkdir, stat } from "node:fs/promises";
import path from "node:path";

type Asset = {
  platform: string;
  arch: string;
  url: string;
  sha256: string;
  size: number;
};

type Options = {
  repo: string;
  tag: string;
  out: string;
  baseUrl: string;
  version?: string;
  notes?: string;
  publishedAt?: string;
  files: string[];
};

function usage(): never {
  console.error(
    [
      "Usage:",
      "  bun run ota:manifest -- --repo owner/repo --tag v0.1.1 --out manifest.json <files...>",
      "",
      "Options:",
      "  --repo owner/repo        GitHub repository used for release asset URLs",
      "  --tag vX.Y.Z             GitHub release tag",
      "  --out file               Manifest output path",
      "  --base-url url           GitHub base URL, defaults to https://github.com",
      "  --version X.Y.Z          Manifest version, defaults to package.json version",
      "  --notes text             Manifest notes",
      "  --published-at date      Manifest publishedAt, defaults to current ISO time",
    ].join("\n"),
  );
  process.exit(1);
}

function parseArgs(argv: string[]): Options {
  const opts: Partial<Options> = {
    baseUrl: "https://github.com",
    files: [],
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const readValue = (name: string): string => {
      const inline = arg.match(new RegExp(`^${name}=(.+)$`));
      if (inline) return inline[1];
      const next = argv[++i];
      if (!next) usage();
      return next;
    };

    if (arg === "--repo" || arg.startsWith("--repo=")) opts.repo = readValue("--repo");
    else if (arg === "--tag" || arg.startsWith("--tag=")) opts.tag = readValue("--tag");
    else if (arg === "--out" || arg.startsWith("--out=")) opts.out = readValue("--out");
    else if (arg === "--base-url" || arg.startsWith("--base-url=")) opts.baseUrl = readValue("--base-url");
    else if (arg === "--version" || arg.startsWith("--version=")) opts.version = readValue("--version");
    else if (arg === "--notes" || arg.startsWith("--notes=")) opts.notes = readValue("--notes");
    else if (arg === "--published-at" || arg.startsWith("--published-at=")) opts.publishedAt = readValue("--published-at");
    else if (arg.startsWith("--")) usage();
    else opts.files!.push(arg);
  }

  if (!opts.repo || !opts.tag || !opts.out || !opts.files?.length) usage();
  if (!/^[^/]+\/[^/]+$/.test(opts.repo)) {
    throw new Error(`Invalid --repo value: ${opts.repo}`);
  }

  return opts as Options;
}

function assetTarget(file: string): { platform: string; arch: string } {
  const name = path.basename(file);
  const match = /^ao3-hub-(darwin|linux|win32)-(x64|arm64)(?:\.exe)?$/.exec(name);
  if (!match) {
    throw new Error(
      `Cannot infer platform/arch from ${name}; expected ao3-hub-<platform>-<arch>`,
    );
  }
  return { platform: match[1], arch: match[2] };
}

async function sha256Hex(file: string): Promise<string> {
  const buf = await Bun.file(file).arrayBuffer();
  const hash = await crypto.subtle.digest("SHA-256", buf);
  return Array.from(new Uint8Array(hash))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

async function packageVersion(): Promise<string> {
  const root = path.resolve(import.meta.dir, "..", "..");
  const pkg = await Bun.file(path.join(root, "package.json")).json();
  return String(pkg.version ?? "0.0.0");
}

async function main() {
  const opts = parseArgs(Bun.argv.slice(2));
  const version = opts.version ?? (await packageVersion());
  const baseUrl = opts.baseUrl.replace(/\/+$/, "");
  const releaseUrl = `${baseUrl}/${opts.repo}/releases/download/${encodeURIComponent(opts.tag)}`;

  const assets: Asset[] = [];
  for (const file of opts.files) {
    const { platform, arch } = assetTarget(file);
    const name = path.basename(file);
    const stats = await stat(file);
    assets.push({
      platform,
      arch,
      url: `${releaseUrl}/${encodeURIComponent(name)}`,
      sha256: await sha256Hex(file),
      size: stats.size,
    });
  }

  assets.sort((a, b) => `${a.platform}/${a.arch}`.localeCompare(`${b.platform}/${b.arch}`));

  const manifest = {
    version,
    notes: opts.notes ?? `GitHub release ${opts.tag}`,
    publishedAt: opts.publishedAt ?? new Date().toISOString(),
    assets,
  };

  await mkdir(path.dirname(opts.out), { recursive: true });
  await Bun.write(opts.out, JSON.stringify(manifest, null, 2) + "\n");
  console.log(`[ota] wrote ${opts.out} with ${assets.length} assets`);
}

await main();
