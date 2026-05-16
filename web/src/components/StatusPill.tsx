import type { StoryStatus } from "@ao3hub/shared";
import { Badge } from "@/components/ui/badge";

const LABEL: Record<StoryStatus, string> = {
  queued: "排队中",
  fetching: "抓取中",
  parsing: "解析中",
  translating: "翻译中",
  ready: "就绪",
  error: "出错",
};

const VARIANT: Record<StoryStatus, "default" | "accent" | "success" | "destructive"> = {
  queued: "default",
  fetching: "default",
  parsing: "default",
  translating: "default",
  ready: "success",
  error: "destructive",
};

export function StatusPill({ status }: { status: StoryStatus }) {
  const inFlight = status !== "ready" && status !== "error";
  return (
    <Badge variant={VARIANT[status]} className="gap-1.5 normal-case tracking-[0.04em]">
      <span
        className={`inline-block size-1.5 rounded-full bg-current ${
          inFlight ? "animate-pulse" : ""
        }`}
        aria-hidden
      />
      {LABEL[status]}
    </Badge>
  );
}
