import type { LlmConfig } from "@ao3hub/shared";

export type ChatMessage = { role: "system" | "user" | "assistant"; content: string };

export type ChatResult = {
  content: string;
  usage?: { prompt_tokens?: number; completion_tokens?: number; total_tokens?: number };
};

export class LlmError extends Error {
  status: number;
  body: string;
  constructor(status: number, body: string) {
    super(`LLM provider error ${status}: ${body.slice(0, 200)}`);
    this.status = status;
    this.body = body;
  }
}

export async function chat(
  config: LlmConfig,
  messages: ChatMessage[],
  opts: { signal?: AbortSignal; jsonMode?: boolean } = {},
): Promise<ChatResult> {
  if (!config.apiKey) throw new Error("LLM apiKey not configured");
  if (!config.baseURL) throw new Error("LLM baseURL not configured");
  const url = config.baseURL.replace(/\/$/, "") + "/chat/completions";
  const body: Record<string, unknown> = {
    model: config.model,
    temperature: config.temperature,
    messages,
  };
  if (opts.jsonMode !== false) body.response_format = { type: "json_object" };

  const res = await fetch(url, {
    method: "POST",
    headers: {
      authorization: `Bearer ${config.apiKey}`,
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
    signal: opts.signal,
  });
  const text = await res.text();
  if (!res.ok) throw new LlmError(res.status, text);

  let json: any;
  try {
    json = JSON.parse(text);
  } catch (e) {
    throw new LlmError(500, `non-json response: ${text.slice(0, 200)}`);
  }
  const content = json?.choices?.[0]?.message?.content;
  if (typeof content !== "string") {
    throw new LlmError(500, `missing message.content: ${text.slice(0, 200)}`);
  }
  return { content, usage: json?.usage };
}
