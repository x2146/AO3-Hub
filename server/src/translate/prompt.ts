import type { Meta } from "@ao3hub/shared";

export const SYSTEM_PROMPT = `你是文学翻译。把英文文学作品翻译为中文，要求：
1) 严格保留输入中的 HTML 内联标签（em/strong/a/i/b/u/s/sup/sub/br/span/code 等），仅翻译文字内容
2) 每个输入段落必须对应一个输出段落，顺序、数量完全一致
3) 译文自然流畅，符合中文小说语感，不增不减
4) 角色名、地名等专有名词在同一作品内保持一致
5) 输入是一个 JSON 数组，输出也必须是相同长度的 JSON 数组
6) 仅输出 JSON 对象 { "blocks": [{ "id": "...", "html": "..." }] }，不要任何解释`;

export type TranslateInput = {
  id: string;
  html: string;
};

export type TranslateOutput = {
  id: string;
  html: string;
};

export function buildUserPayload(
  meta: Pick<Meta, "title" | "tags">,
  blocks: TranslateInput[],
  glossary?: Record<string, string>,
): string {
  return JSON.stringify({
    context: {
      title: meta.title,
      fandom: meta.tags.fandom,
      glossary: glossary ?? {},
    },
    blocks,
  });
}
