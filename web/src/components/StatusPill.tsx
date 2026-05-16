import type { StoryStatus } from "@ao3hub/shared";

const LABEL: Record<StoryStatus, string> = {
  queued: "排队中",
  fetching: "抓取中",
  parsing: "解析中",
  translating: "翻译中",
  ready: "就绪",
  error: "出错",
};

export function StatusPill({ status }: { status: StoryStatus }) {
  const cls =
    status === "ready"
      ? "chip-accent"
      : status === "error"
        ? "chip-error"
        : "";
  return (
    <span className={`chip ${cls}`}>
      <span className="dot" aria-hidden />
      {LABEL[status]}
    </span>
  );
}
