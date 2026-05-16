import { mkdir, rename } from "node:fs/promises";
import path from "node:path";

async function ensureDir(p: string): Promise<void> {
  await mkdir(path.dirname(p), { recursive: true });
}

export async function readJson<T>(file: string): Promise<T | null> {
  const f = Bun.file(file);
  if (!(await f.exists())) return null;
  try {
    return (await f.json()) as T;
  } catch {
    return null;
  }
}

export async function writeJson(file: string, data: unknown): Promise<void> {
  await ensureDir(file);
  const tmp = file + ".tmp";
  await Bun.write(tmp, JSON.stringify(data, null, 2));
  await rename(tmp, file);
}

export async function writeText(file: string, data: string): Promise<void> {
  await ensureDir(file);
  await Bun.write(file, data);
}

export async function readText(file: string): Promise<string | null> {
  const f = Bun.file(file);
  if (!(await f.exists())) return null;
  return await f.text();
}
