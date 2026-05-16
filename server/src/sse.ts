import type { StreamEvent } from "@ao3hub/shared";

type Listener = (event: StreamEvent) => void;
const channels = new Map<string, Set<Listener>>();

export function subscribe(storyId: string, fn: Listener): () => void {
  let set = channels.get(storyId);
  if (!set) {
    set = new Set();
    channels.set(storyId, set);
  }
  set.add(fn);
  return () => {
    const s = channels.get(storyId);
    if (!s) return;
    s.delete(fn);
    if (s.size === 0) channels.delete(storyId);
  };
}

export function emit(storyId: string, event: StreamEvent): void {
  const set = channels.get(storyId);
  if (!set) return;
  for (const fn of set) {
    try {
      fn(event);
    } catch {}
  }
}

export function listenerCount(storyId: string): number {
  return channels.get(storyId)?.size ?? 0;
}
