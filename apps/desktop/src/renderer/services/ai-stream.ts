/**
 * SSE 流式响应解析。
 *
 * 逐行解析 `data: {...}` 事件并把增量分发给回调。长回答会产生成百上千个
 * delta，累积期间要定期让出，否则渲染进程主线程被占满、界面卡住。
 */
import type {
  ChatCompletionResult,
  StreamCallbacks,
} from "./ai-types";

/** 累计多少个 delta 之后让出一次事件循环 */
const YIELD_EVERY_DELTAS = 20;

export interface StreamState {
  fullContent: string;
  thinkingContent: string;
  buffer: string;
  chunkCount: number;
}

export function createStreamState(): StreamState {
  return {
    fullContent: "",
    thinkingContent: "",
    buffer: "",
    chunkCount: 0,
  };
}

function yieldToEventLoop(): Promise<void> {
  return new Promise<void>((resolve) => {
    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(() => resolve());
    } else {
      setTimeout(resolve, 0);
    }
  });
}

export async function processStreamTextChunk(
  chunkText: string,
  state: StreamState,
  onStream?: (chunk: string) => void,
  streamCallbacks?: StreamCallbacks,
  options?: {
    flush?: boolean;
    yieldToUi?: boolean;
  },
): Promise<void> {
  state.buffer += chunkText;
  const lines = state.buffer.split("\n");
  // 非 flush 时最后一行可能是半条事件，留在缓冲里等下一块
  state.buffer = options?.flush ? "" : lines.pop() || "";
  let deltasSinceYield = 0;

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed === "data: [DONE]") continue;
    if (!trimmed.startsWith("data: ")) continue;

    try {
      const json = JSON.parse(trimmed.slice(6));
      const delta = json.choices?.[0]?.delta;

      if (!delta) {
        continue;
      }

      state.chunkCount++;
      deltasSinceYield++;

      if (delta.reasoning_content) {
        state.thinkingContent += delta.reasoning_content;
        streamCallbacks?.onThinking?.(delta.reasoning_content);
      }

      if (delta.content) {
        state.fullContent += delta.content;
        onStream?.(delta.content);
        streamCallbacks?.onContent?.(delta.content);
      }

      if (options?.yieldToUi && deltasSinceYield >= YIELD_EVERY_DELTAS) {
        deltasSinceYield = 0;
        await yieldToEventLoop();
      }
    } catch {
      // 忽略解析错误 / Ignore parse errors
    }
  }

  if (options?.yieldToUi) {
    await yieldToEventLoop();
  }
}

export function finalizeStreamState(
  state: StreamState,
  streamCallbacks?: StreamCallbacks,
): ChatCompletionResult {
  streamCallbacks?.onComplete?.(
    state.fullContent,
    state.thinkingContent || undefined,
  );

  return {
    content: state.fullContent,
    thinkingContent: state.thinkingContent || undefined,
  };
}

/** web 回退路径：直接读 fetch 的 ReadableStream */
export async function handleStreamResponse(
  response: Response,
  onStream?: (chunk: string) => void,
  streamCallbacks?: StreamCallbacks,
): Promise<ChatCompletionResult> {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error("无法读取响应流");
  }

  const decoder = new TextDecoder();
  const state = createStreamState();

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }

      await processStreamTextChunk(
        decoder.decode(value, { stream: true }),
        state,
        onStream,
        streamCallbacks,
        { yieldToUi: true },
      );
    }

    await processStreamTextChunk(
      decoder.decode(),
      state,
      onStream,
      streamCallbacks,
      { flush: true, yieldToUi: true },
    );
  } finally {
    reader.releaseLock();
  }

  return finalizeStreamState(state, streamCallbacks);
}
