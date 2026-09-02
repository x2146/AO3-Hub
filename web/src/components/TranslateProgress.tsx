import type { Progress } from "@ao3hub/shared";
import { cn } from "@/lib/utils";

export type ProgressBreakdown = {
  total: number;
  done: number;
  error: number;
  inflight: number;
  pending: number;
};

export function breakdownOf(progress: Progress | undefined | null): ProgressBreakdown {
  const total = progress?.totalBlocks ?? 0;
  const done = Math.min(progress?.doneBlocks ?? 0, total);
  const error = Math.min(progress?.errorBlocks ?? 0, Math.max(0, total - done));
  const inflight = Math.min(
    progress?.inflightBlocks ?? 0,
    Math.max(0, total - done - error),
  );
  const pending = Math.max(0, total - done - error - inflight);
  return { total, done, error, inflight, pending };
}

export function TranslateProgressBar({
  progress,
  className,
  thin = false,
}: {
  progress: Progress | undefined | null;
  className?: string;
  thin?: boolean;
}) {
  const { total, done, error, inflight, pending } = breakdownOf(progress);
  const trackClass = cn(
    "flex w-full overflow-hidden rounded-full bg-muted",
    thin ? "h-1" : "h-1.5",
    className,
  );

  if (total === 0) return <div className={trackClass} />;

  const pct = (n: number) => `${(n / total) * 100}%`;
  return (
    <div
      className={trackClass}
      role="progressbar"
      aria-label="翻译进度"
      aria-valuemin={0}
      aria-valuemax={total}
      aria-valuenow={done}
      aria-valuetext={`${done} / ${total} 段已完成`}
    >
      {done > 0 && <div className="h-full bg-success" style={{ width: pct(done) }} />}
      {inflight > 0 && (
        <div
          className="h-full animate-pulse bg-primary"
          style={{ width: pct(inflight) }}
        />
      )}
      {error > 0 && (
        <div className="h-full bg-destructive" style={{ width: pct(error) }} />
      )}
      {pending > 0 && (
        <div
          className="h-full bg-muted-foreground/25"
          style={{ width: pct(pending) }}
        />
      )}
    </div>
  );
}

export function TranslateProgressLegend({
  progress,
  className,
}: {
  progress: Progress | undefined | null;
  className?: string;
}) {
  const { total, done, error, inflight, pending } = breakdownOf(progress);
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-muted-foreground",
        className,
      )}
    >
      <span className="font-medium text-foreground">
        {done}/{total} · {pct}%
      </span>
      <Chip color="bg-primary" label="进行" value={inflight} />
      <Chip color="bg-success" label="成功" value={done} />
      <Chip color="bg-destructive" label="失败" value={error} />
      <Chip color="bg-muted-foreground/40" label="等待" value={pending} />
    </div>
  );
}

/** Zero-valued buckets are dropped so the legend stays scannable mid-run. */
function Chip({
  color,
  label,
  value,
}: {
  color: string;
  label: string;
  value: number;
}) {
  if (value === 0) return null;
  return (
    <span className="inline-flex items-center gap-1">
      <span className={cn("inline-block size-1.5 rounded-full", color)} aria-hidden />
      <span>
        {label} {value}
      </span>
    </span>
  );
}
