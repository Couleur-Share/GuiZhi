import type { ReadingPageRecord } from "@guizhi/shared/types/reading-page-v3";
import { readingUnitId } from "@guizhi/shared/utils/reading-page-v3";

/** JSONL 只交付完整记录；结束标记与 HTTP 成功分别验证。 */
export class ReadingRecordStream {
  private buffer = "";
  private size = 0;
  private meta = false;
  private done = false;
  private ids = new Set<string>();
  constructor(private consume: (record: ReadingPageRecord) => void) {}
  push(delta: string) {
    this.size += delta.length;
    if (this.size > 4000000) throw new Error("阅读页流式输出超过大小上限");
    this.buffer += delta;
    let at: number;
    while ((at = this.buffer.indexOf("\n")) >= 0) {
      const line = this.buffer.slice(0, at).trim();
      this.buffer = this.buffer.slice(at + 1);
      this.line(line);
    }
  }
  private line(line: string) {
    if (!line || /^```(?:jsonl|json)?$/.test(line)) return;
    if (this.done) throw new Error("阅读页结束标记后出现额外记录");
    const r = JSON.parse(line);
    if (!r || typeof r !== "object" || Array.isArray(r))
      throw new Error("阅读页记录必须是对象");
    if (r.type === "meta") {
      if (this.meta || typeof r.css !== "string")
        throw new Error("阅读页主题记录重复或无效");
      this.meta = true;
    } else {
      if (!this.meta) throw new Error("阅读页记录缺少前置主题");
      if (r.type === "done") this.done = true;
      else if (r.type === "section" || r.type === "script") {
        if (
          !readingUnitId(r.id) ||
          this.ids.has(r.id) ||
          typeof r[r.type === "section" ? "html" : "code"] !== "string"
        )
          throw new Error("阅读页单元 ID 重复或记录无效");
        this.ids.add(r.id);
      } else throw new Error("未知阅读页记录类型");
    }
    this.consume(r);
  }
  finish() {
    this.line(this.buffer.trim());
    this.buffer = "";
    if (!this.done)
      throw new Error("阅读页流式输出缺少完成标记，已完成章节已保留");
  }
}

/** 有界工作池：失败后不再派发，等待在途任务完成其检查点再抛错。 */
export async function readingPool<T>(
  values: T[],
  limit: number,
  run: (value: T, index: number) => Promise<void>,
): Promise<void> {
  let cursor = 0;
  let failure: unknown;
  const workers = Array.from(
    { length: Math.min(limit, values.length) },
    async () => {
      while (!failure && cursor < values.length) {
        const i = cursor++;
        try {
          await run(values[i], i);
        } catch (e) {
          failure ??= e;
        }
      }
    },
  );
  await Promise.all(workers);
  if (failure) throw failure;
}
