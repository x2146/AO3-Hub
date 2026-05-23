import { existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import { BUILD_VERSION, BUILD_BUILT_AT } from "./embedded";

const fromEnv = process.env.AO3HUB_DATA_DIR?.trim();
const fromCwd = path.resolve(process.cwd(), "data");
export const DATA_DIR = fromEnv ? path.resolve(fromEnv) : fromCwd;

if (!existsSync(DATA_DIR)) {
  mkdirSync(DATA_DIR, { recursive: true });
}

export const VERSION = BUILD_VERSION ?? process.env.AO3HUB_VERSION ?? "0.1.0-dev";
export const BUILT_AT =
  BUILD_BUILT_AT ?? process.env.AO3HUB_BUILT_AT ?? new Date().toISOString();
export const PLATFORM = `${process.platform}`;
export const ARCH = `${process.arch}`;

export function resolveHost(configHost: string): string {
  return process.env.HOST?.trim() || configHost;
}

export function resolvePort(configPort: number): number {
  const raw = process.env.PORT?.trim();
  if (!raw) return configPort;

  const port = Number(raw);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid PORT: ${raw}`);
  }
  return port;
}
