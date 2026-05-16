import path from "node:path";
import { DATA_DIR } from "../env";

export function storyDir(id: string): string {
  return path.join(DATA_DIR, "stories", id);
}

export const paths = {
  config: () => path.join(DATA_DIR, "config.json"),
  index: () => path.join(DATA_DIR, "index.json"),
  users: () => path.join(DATA_DIR, "users.json"),
  sessions: () => path.join(DATA_DIR, "sessions.json"),
  story: (id: string) => storyDir(id),
  meta: (id: string) => path.join(storyDir(id), "meta.json"),
  source: (id: string) => path.join(storyDir(id), "source.html"),
  original: (id: string) => path.join(storyDir(id), "original.json"),
  translated: (id: string) => path.join(storyDir(id), "translated.json"),
  progress: (id: string) => path.join(storyDir(id), "progress.json"),
};
