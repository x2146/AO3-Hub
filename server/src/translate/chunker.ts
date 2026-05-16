import type { Block } from "@ao3hub/shared";

export type Batch = {
  blocks: Block[];
};

function approxTokens(text: string): number {
  return Math.ceil(text.length / 3.2);
}

export function chunk(blocks: Block[], blocksPerRequest: number, maxTokens = 3500): Batch[] {
  const batches: Batch[] = [];
  let current: Block[] = [];
  let currentTokens = 0;
  for (const b of blocks) {
    const t = approxTokens(b.html);
    const wouldExceed =
      current.length >= blocksPerRequest || (current.length > 0 && currentTokens + t > maxTokens);
    if (wouldExceed) {
      batches.push({ blocks: current });
      current = [];
      currentTokens = 0;
    }
    current.push(b);
    currentTokens += t;
  }
  if (current.length) batches.push({ blocks: current });
  return batches;
}

export function isTranslatable(b: Block): boolean {
  if (b.type === "hr") return false;
  if (!b.html.trim()) return false;
  return true;
}
