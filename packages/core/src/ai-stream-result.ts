import type { AIProtocol } from "@guizhi/shared/types";
import { extractUsageFromChatResponse } from "@guizhi/shared/utils/ai-protocol";
import type { AIChatResult } from "./ai-client";

/** 收集受限 SSE 文本；只在完整结束后把结果交给调用方。 */
export async function readChatStream(response: Response, protocol: AIProtocol, onProgress?: (receivedChars: number) => void, onDelta?: (text: string) => void): Promise<AIChatResult> {
  if (!response.body) throw new Error("模型流式响应为空");
  const reader = response.body.getReader(), decoder = new TextDecoder();
  let buffer = "", content = "", finished = false, terminal = false, bytes = 0, finishReason: string | undefined;
  let usage: Record<string,unknown> = {};
  const event = (block: string) => {
    const data = block.split("\n").filter(line=>line.startsWith("data:")).map(line=>line.slice(5).trimStart()).join("\n");
    if (!data) return;
    if (data === "[DONE]") { finished = true; terminal = true; return; }
    const value = JSON.parse(data);
    if (value.error || value.type === "error") throw new Error("模型流式响应报告错误");
    const previousLength = content.length;
    if (protocol === "anthropic") {
      if (value.type === "content_block_delta" && value.delta?.type === "text_delta") content += value.delta.text ?? "";
      if (value.type === "message_start") usage = {...usage,...value.message?.usage};
      if (value.type === "message_delta") {usage={...usage,...value.usage};finishReason=value.delta?.stop_reason;}
      if (value.type === "message_stop") { finished = true; terminal = true; }
    } else {
      const choice = value.choices?.[0];
      if (typeof choice?.delta?.content === "string") content += choice.delta.content;
      if (choice?.finish_reason) {finishReason=choice.finish_reason;finished=true;}
      if (value.usage) usage=value.usage;
    }
    if (content.length !== previousLength) { onProgress?.(content.length); onDelta?.(content.slice(previousLength)); }
  };
  try {
    for (;;) {
      const part = await reader.read();
      if (part.done) break;
      bytes += part.value.byteLength; if (bytes > 8 * 1024 * 1024) throw new Error("模型响应超过大小上限");
      buffer += decoder.decode(part.value,{stream:true});
      // SSE 允许 CRLF，保留分块边界上的 CR，等 LF 到达后再归一。
      buffer = buffer.replace(/\r\n/g,"\n");
      let index: number;
      while (!terminal && (index=buffer.indexOf("\n\n")) >= 0) {event(buffer.slice(0,index));buffer=buffer.slice(index+2);}
      // 结束标记后主动回收连接；代理可能不会立即关闭 SSE 响应。
      if (terminal) break;
    }
    buffer += decoder.decode(); if (!terminal && buffer.trim()) event(buffer);
    if (!finished || !content.trim()) throw new Error("模型流式响应未完整结束，请重试");
    return {content,finishReason:finishReason==="max_tokens"?"length":finishReason,usage:extractUsageFromChatResponse({usage},protocol)};
  } finally { await reader.cancel().catch(()=>{}); reader.releaseLock(); }
}
