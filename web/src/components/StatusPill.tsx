import type { StoryStatus } from "@ao3hub/shared";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { PHASE_LABEL, isInFlight } from "@/lib/status";

const VARIANT: Record<StoryStatus, "secondary" | "accent" | "success" | "destructive"> = {
  queued: "secondary",
  fetching: "accent",
  parsing: "accent",
  analyzing: "accent",
  translating: "accent",
  ready: "success",
  error: "destructive",
};

export function StatusPill({ status }: { status: StoryStatus }) {
  const inFlight = isInFlight(status);
  return (
    <Badge
      variant={VARIANT[status]}
      role="status"
      aria-live={inFlight ? "polite" : "off"}
    >
      <span
        className={cn(
          "inline-block size-1.5 rounded-full bg-current",
          inFlight && "animate-pulse",
        )}
        aria-hidden
      />
      {PHASE_LABEL[status]}
    </Badge>
  );
}
