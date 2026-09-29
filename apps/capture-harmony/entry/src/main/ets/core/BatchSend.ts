import { Draft, LocalState, Receipt } from './Protocol';
import { CaptureSender } from './DraftQueue';
import { RelayRequestError } from './ConnectionCheck';
import { failureDetail } from './SendOutcome';
import { previewText } from './TextPreview';

// 批量发送所需的队列能力。单独声明成接口，测试可用内存替身覆盖各种结局。
export interface BatchQueue {
  snapshot(): LocalState;
  isSending(requestId: string): boolean;
  send(requestId: string, binding: string, sender: CaptureSender): Promise<Receipt>;
}

export class BatchFailure {
  requestId: string = '';
  preview: string = '';
  message: string = '';
}

export class BatchSkip {
  requestId: string = '';
  preview: string = '';
  reason: string = '';
}

export class BatchResult {
  // 开始时收集箱里的条数（含之后被跳过的）。
  total: number = 0;
  sent: string[] = [];
  failed: BatchFailure[] = [];
  skipped: BatchSkip[] = [];
  // 触发提前中止的错误码（如 rate_limited、network），未中止为空。
  stoppedBy: string = '';
  stoppedMessage: string = '';
  // 全部成功才算成功：任何失败、跳过或中止都不能汇报为成功。
  get ok(): boolean {
    return this.failed.length === 0 && this.skipped.length === 0 && this.stoppedBy === '';
  }
}

// 页面接入点：开始/结束某一条时更新界面。用带默认实现的字段承载回调，兼容 ArkTS 对对象字面量的限制。
export class BatchHooks {
  started: (requestId: string) => void = (requestId: string): void => {};
  // 每处理完一条（含被跳过的）调用一次，done 为已处理条数。
  finished: (requestId: string, done: number, total: number) => void = (requestId: string, done: number, total: number): void => {};
  // 一条发送成功后调用（回执已确认、草稿已移除），用于留存发送记录。
  // 实现方必须自行处理失败：这里抛出的异常会中断整批发送，也不能因此把已成功的发送汇报成失败。
  sent: (draft: Draft, receipt: Receipt) => Promise<void> = async (draft: Draft, receipt: Receipt): Promise<void> => {};
}

export const SKIP_OTHER_COMPUTER: string = '曾发往另一台电脑，请先核对原电脑的收集记录';
export const SKIP_STOPPED: string = '前面的发送已中止，这条尚未发送';

const PREVIEW_LENGTH: number = 28;

// 只有“这条内容自身有问题”的拒绝（400/413）不影响后面的条目。
// 配额、限流、认证、网络、服务故障、回执异常、落盘失败对后面的条目同样成立，继续发送只会重复失败并消耗限流额度。
function isItemSpecific(error: Error): boolean {
  if (!(error instanceof RelayRequestError)) return false;
  return error.status === 400 || error.status === 413;
}

function errorCode(error: Error): string {
  if (error instanceof RelayRequestError && error.code) return error.code;
  return 'error';
}

function findDraft(state: LocalState, requestId: string): Draft | undefined {
  for (let i = 0; i < state.drafts.length; i++) {
    if (state.drafts[i].requestId === requestId) return state.drafts[i];
  }
  return undefined;
}

// 逐条串行发送收集箱里的草稿（按创建先后）。
// - 不占用页面锁：sender 每条的网络请求之间，界面仍可继续记录。
// - 已被其他路径发送或移除的条目不算失败，由对应路径负责报告。
// - 绑定其他电脑的草稿跳过并注明原因；遇到对后续条目同样成立的错误立即停止，其余条目标记为“未发送”。
export async function sendBatch(queue: BatchQueue, binding: string, sender: CaptureSender, hooks: BatchHooks): Promise<BatchResult> {
  const result = new BatchResult();
  const ids: string[] = [];
  const initial = queue.snapshot().drafts;
  for (let i = 0; i < initial.length; i++) ids.push(initial[i].requestId);
  result.total = ids.length;
  let done = 0;
  for (let i = 0; i < ids.length; i++) {
    const requestId = ids[i];
    const draft = findDraft(queue.snapshot(), requestId);
    if (draft !== undefined) {
      if (result.stoppedBy !== '') {
        const skip = new BatchSkip();
        skip.requestId = requestId; skip.preview = previewText(draft.input, PREVIEW_LENGTH); skip.reason = SKIP_STOPPED;
        result.skipped.push(skip);
      } else if (draft.attempted && draft.binding !== binding) {
        const skip = new BatchSkip();
        skip.requestId = requestId; skip.preview = previewText(draft.input, PREVIEW_LENGTH); skip.reason = SKIP_OTHER_COMPUTER;
        result.skipped.push(skip);
      } else if (!queue.isSending(requestId)) {
        hooks.started(requestId);
        let receipt: Receipt | undefined = undefined;
        let failure: Error | undefined = undefined;
        try {
          receipt = await queue.send(requestId, binding, sender);
        } catch (error) {
          failure = error as Error;
        }
        if (failure === undefined) {
          result.sent.push(requestId);
          // 放在 try 之外：发送记录的问题不能改变这条“已发送”的结论。
          await hooks.sent(draft, receipt as Receipt);
        } else {
          const item = new BatchFailure();
          item.requestId = requestId; item.preview = previewText(draft.input, PREVIEW_LENGTH);
          item.message = failureDetail(failure);
          result.failed.push(item);
          if (!isItemSpecific(failure)) {
            result.stoppedBy = errorCode(failure);
            result.stoppedMessage = item.message;
          }
        }
      }
    }
    done += 1;
    hooks.finished(requestId, done, result.total);
  }
  return result;
}

// 一句话汇总：全部成功才使用成功措辞。
export function batchHeadline(result: BatchResult): string {
  if (result.ok) return `已发送 ${result.sent.length} 条，中转已接收，电脑上线后将处理`;
  const parts: string[] = [`已发送 ${result.sent.length} 条`];
  if (result.failed.length > 0) parts.push(`失败 ${result.failed.length} 条`);
  if (result.skipped.length > 0) parts.push(`跳过 ${result.skipped.length} 条`);
  const summary = parts.join('，');
  return result.stoppedBy !== '' ? `${summary}。已停止：${result.stoppedMessage}` : summary;
}

function lines(title: string, items: string[], limit: number): string[] {
  if (items.length === 0) return [];
  const out: string[] = [`${title}（${items.length} 条）：`];
  const shown = limit > 0 ? Math.min(limit, items.length) : items.length;
  for (let i = 0; i < shown; i++) out.push(`· ${items[i]}`);
  if (shown < items.length) out.push(`· 另有 ${items.length - shown} 条，点击“复制原因”查看全部`);
  return out;
}

// 逐条原因。limit > 0 时每类最多显示这么多条（界面内联展示用），0 表示全部（复制用）。
export function batchReport(result: BatchResult, limit: number): string {
  const failed: string[] = [];
  for (let i = 0; i < result.failed.length; i++) failed.push(`${result.failed[i].preview} — ${result.failed[i].message}`);
  const skipped: string[] = [];
  for (let i = 0; i < result.skipped.length; i++) skipped.push(`${result.skipped[i].preview} — ${result.skipped[i].reason}`);
  return lines('失败', failed, limit).concat(lines('跳过', skipped, limit)).join('\n');
}
