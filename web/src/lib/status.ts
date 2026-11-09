import type { StoryStatus, LlmCallStage } from "@ao3hub/shared";

/**
 * Canonical Chinese labels for every story phase — the single source of truth.
 * The StatusPill, the Reader chapter-progress box, and anything else that names
 * a phase read from here, so the wording can never drift again (the Reader used
 * to say "预读分析中" while the pill said "预读中").
 *
 * `ProgressPhase` shares the same union of values as `StoryStatus`, so this
 * record can be indexed by either type.
 */
export const PHASE_LABEL: Record<StoryStatus, string> = {
  queued: "排队中",
  fetching: "抓取中",
  parsing: "解析中",
  analyzing: "预读中",
  translating: "翻译中",
  ready: "就绪",
  error: "出错",
};

/**
 * What the run is doing before any paragraph has been translated, for the
 * phases whose progress bar would otherwise sit empty with no explanation.
 */
export const PHASE_HINT: Partial<Record<StoryStatus, string>> = {
  queued: "等待前面的作品处理完，随后自动开始。",
  fetching: "正在从 AO3 下载作品。",
  parsing: "正在拆分章节与段落。",
  analyzing: "先通读全文、整理人名与术语，译文会更连贯。",
};

/** A phase is "in flight" while work is ongoing — every state but the two terminal ones. */
export function isInFlight(phase: StoryStatus): boolean {
  return phase !== "ready" && phase !== "error";
}

/** Canonical labels for the LLM-call stages shown in the translation-status panel. */
export const STAGE_LABEL: Record<LlmCallStage, string> = {
  "analysis-chapter": "分章预读",
  "analysis-merge": "归并",
  "analysis-full": "全文预读",
  "translate-batch": "翻译批次",
};
