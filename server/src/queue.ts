type JobType = "translate" | "retry";

export type Job = {
  storyId: string;
  type: JobType;
  payload?: { blockIds?: string[]; chapterIndex?: number };
};

const pending: Job[] = [];
const inflight = new Set<string>();
let processor: ((job: Job) => Promise<void>) | null = null;

export function setProcessor(fn: (job: Job) => Promise<void>): void {
  processor = fn;
}

export function enqueue(job: Job): void {
  pending.push(job);
  pump();
}

export function snapshot(): { pending: Job[]; inflight: string[] } {
  return { pending: [...pending], inflight: [...inflight] };
}

let pumping = false;
async function pump(): Promise<void> {
  if (pumping) return;
  if (!processor) return;
  pumping = true;
  try {
    while (pending.length) {
      const job = pending.shift()!;
      if (inflight.has(job.storyId)) {
        pending.push(job);
        const stillBlocked = pending.every((j) => inflight.has(j.storyId));
        if (stillBlocked) break;
        continue;
      }
      inflight.add(job.storyId);
      try {
        await processor(job);
      } catch (e) {
        console.error("[queue] job failed", job, e);
      } finally {
        inflight.delete(job.storyId);
      }
    }
  } finally {
    pumping = false;
    if (pending.length) setTimeout(pump, 0);
  }
}
