import type { StoryStatus } from "@ao3hub/shared";
import { Badge } from "@/components/ui/badge";
import { PHASE_LABEL, isInFlight } from "@/lib/status";

const VARIANT: Record<StoryStatus, "default" | "accent" | "success" | "destructive"> = {
  queued: "default",
  fetching: "default",
  parsing: "default",
  analyzing: "default",
  translating: "default",
  ready: "success",
  error: "destructive",
};

export function StatusPill({ status }: { status: StoryStatus }) {
  return (
    <Badge variant={VARIANT[status]} className="gap-1.5 normal-case tracking-[0.04em]">
      <span
        className={`inline-block size-1.5 rounded-full bg-current ${
          isInFlight(status) ? "animate-pulse" : ""
        }`}
        aria-hidden
      />
      {PHASE_LABEL[status]}
    </Badge>
  );
}
