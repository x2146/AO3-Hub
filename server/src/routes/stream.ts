import { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import type { StreamEvent } from "@ao3hub/shared";
import { story } from "../db";
import { subscribe } from "../sse";

const r = new Hono();

r.get("/:id/stream", async (c) => {
  const id = c.req.param("id");
  if (!(await story.exists(id))) return c.text("not found", 404);

  return streamSSE(c, async (stream) => {
    const progress = await story.loadProgress(id);
    if (progress) {
      const event: StreamEvent = {
        type: "progress",
        doneBlocks: progress.doneBlocks,
        totalBlocks: progress.totalBlocks,
        phase: progress.phase,
      };
      await stream.writeSSE({ event: "progress", data: JSON.stringify(event) });
      await stream.writeSSE({
        event: "phase",
        data: JSON.stringify({ type: "phase", phase: progress.phase }),
      });
    }

    const queue: StreamEvent[] = [];
    let resolveOne: ((v: StreamEvent | "close") => void) | null = null;

    const unsubscribe = subscribe(id, (event) => {
      if (resolveOne) {
        resolveOne(event);
        resolveOne = null;
      } else {
        queue.push(event);
      }
    });

    const abort = c.req.raw.signal;
    const aborted = new Promise<"close">((resolve) => {
      if (abort.aborted) resolve("close");
      else abort.addEventListener("abort", () => resolve("close"), { once: true });
    });

    const heartbeat = setInterval(() => {
      stream.writeSSE({ event: "ping", data: String(Date.now()) }).catch(() => {});
    }, 15000);

    try {
      while (!abort.aborted) {
        let next: StreamEvent | "close";
        if (queue.length) {
          next = queue.shift()!;
        } else {
          next = await Promise.race<StreamEvent | "close">([
            new Promise<StreamEvent>((res) => (resolveOne = res as any)),
            aborted,
          ]);
        }
        if (next === "close") break;
        try {
          await stream.writeSSE({ event: next.type, data: JSON.stringify(next) });
        } catch {
          break;
        }
      }
    } finally {
      clearInterval(heartbeat);
      unsubscribe();
    }
  });
});

export default r;
