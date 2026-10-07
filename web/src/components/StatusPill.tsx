import type { StoryStatus } from "@ao3hub/shared";
import { CheckCircle2, CircleAlert, Clock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
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
      {status === "ready" ? (
        <CheckCircle2 data-icon="inline-start" />
      ) : status === "error" ? (
        <CircleAlert data-icon="inline-start" />
      ) : status === "queued" ? (
        <Clock data-icon="inline-start" />
      ) : (
        <Spinner data-icon="inline-start" aria-hidden />
      )}
      {PHASE_LABEL[status]}
    </Badge>
  );
}
