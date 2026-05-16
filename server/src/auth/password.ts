export async function hashPassword(plain: string): Promise<string> {
  return await Bun.password.hash(plain, { algorithm: "argon2id" });
}

export async function verifyPassword(plain: string, hash: string): Promise<boolean> {
  if (!hash) return false;
  try {
    return await Bun.password.verify(plain, hash);
  } catch {
    return false;
  }
}
