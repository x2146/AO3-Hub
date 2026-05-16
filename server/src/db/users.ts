import { z } from "zod";
import { Role, type PublicUser } from "@ao3hub/shared";
import { paths } from "./paths";
import { readJson, writeJson } from "./io";

const UserRecord = z.object({
  id: z.string(),
  username: z.string(),
  passwordHash: z.string(),
  role: Role,
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type UserRecord = z.infer<typeof UserRecord>;

const UsersFile = z.object({
  users: z.array(UserRecord).default([]),
});
type UsersFile = z.infer<typeof UsersFile>;

const EMPTY: UsersFile = { users: [] };

async function load(): Promise<UsersFile> {
  const raw = await readJson<unknown>(paths.users());
  if (!raw) return { users: [] };
  const parsed = UsersFile.safeParse(raw);
  return parsed.success ? parsed.data : EMPTY;
}

async function save(file: UsersFile): Promise<void> {
  await writeJson(paths.users(), file);
}

function toPublic(u: UserRecord): PublicUser {
  return {
    id: u.id,
    username: u.username,
    role: u.role,
    createdAt: u.createdAt,
    updatedAt: u.updatedAt,
  };
}

function newId(): string {
  return "u_" + crypto.randomUUID().replace(/-/g, "").slice(0, 16);
}

export const users = {
  toPublic,

  count: async () => (await load()).users.length,

  listPublic: async (): Promise<PublicUser[]> =>
    (await load()).users
      .map(toPublic)
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt)),

  findById: async (id: string): Promise<UserRecord | null> => {
    const file = await load();
    return file.users.find((u) => u.id === id) ?? null;
  },

  findByUsername: async (username: string): Promise<UserRecord | null> => {
    const file = await load();
    const lower = username.toLowerCase();
    return file.users.find((u) => u.username.toLowerCase() === lower) ?? null;
  },

  create: async (input: {
    username: string;
    passwordHash: string;
    role: "admin" | "user";
  }): Promise<UserRecord> => {
    const file = await load();
    const lower = input.username.toLowerCase();
    if (file.users.some((u) => u.username.toLowerCase() === lower)) {
      throw new Error("用户名已存在");
    }
    const now = new Date().toISOString();
    const record: UserRecord = {
      id: newId(),
      username: input.username,
      passwordHash: input.passwordHash,
      role: input.role,
      createdAt: now,
      updatedAt: now,
    };
    file.users.push(record);
    await save(file);
    return record;
  },

  update: async (
    id: string,
    patch: { passwordHash?: string; role?: "admin" | "user" },
  ): Promise<UserRecord | null> => {
    const file = await load();
    const i = file.users.findIndex((u) => u.id === id);
    if (i < 0) return null;
    const next: UserRecord = {
      ...file.users[i],
      ...(patch.passwordHash ? { passwordHash: patch.passwordHash } : {}),
      ...(patch.role ? { role: patch.role } : {}),
      updatedAt: new Date().toISOString(),
    };
    file.users[i] = next;
    await save(file);
    return next;
  },

  remove: async (id: string): Promise<boolean> => {
    const file = await load();
    const before = file.users.length;
    file.users = file.users.filter((u) => u.id !== id);
    if (file.users.length === before) return false;
    await save(file);
    return true;
  },

  adminCount: async (): Promise<number> =>
    (await load()).users.filter((u) => u.role === "admin").length,
};
