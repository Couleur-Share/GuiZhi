import { Draft, LocalState, CaptureMode, Receipt, Submission, validateInput } from './Protocol';
import { classifySendFailure, failureDetail } from './SendOutcome';

export interface StateStore { save(state: LocalState): Promise<void> }
export interface CaptureSender { send(body: Submission): Promise<Receipt> }

// 单条草稿发送的第一步（串行、已落盘）产物：请求体与发送前的标记，供网络结果返回后回滚使用。
class SendStart {
  body: Submission = new Submission();
  priorAttempted: boolean = false;
  priorBinding: string = '';
}

// 所有读改写串行；只有落盘成功才更新内存，网络响应丢失时保留编号与原文。
// 网络请求本身不占用串行链：某条草稿发送期间，编辑、新建、分享落盘和其他草稿的发送照常进行。
export class DraftQueue {
  private state: LocalState;
  private store: StateStore;
  private tail: Promise<void> = Promise.resolve();
  // 正在网络发送中的草稿编号：同一条不允许并发发送，也不允许在结果回来前删除或改为可编辑。
  private inFlight: Set<string> = new Set<string>();
  constructor(store: StateStore, state: LocalState) { this.store = store; this.state = state; }

  snapshot(): LocalState { return JSON.parse(JSON.stringify(this.state)) as LocalState; }

  isSending(requestId: string): boolean { return this.inFlight.has(requestId); }

  private serial<T>(action: () => Promise<T>): Promise<T> {
    const next: Promise<T> = this.tail.then(action);
    this.tail = next.then((): void => {}, (): void => {}); // 调用者接收失败；后续操作仍可重试。
    return next;
  }

  private async commit(next: LocalState): Promise<void> {
    await this.store.save(next);
    this.state = next;
  }

  edit(input: string, mode: CaptureMode): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      const next = this.snapshot(); next.editor = input; next.mode = mode;
      await this.commit(next);
    });
  }

  enqueue(requestId: string): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      validateInput(this.state.editor);
      const next = this.snapshot();
      if (next.drafts.length >= 100) throw new Error('本机已有 100 条草稿，请先发送或清理');
      const draft = new Draft();
      draft.requestId = requestId; draft.input = next.editor; draft.mode = next.mode; draft.createdAt = Date.now();
      next.drafts.push(draft); next.editor = '';
      await this.commit(next);
    });
  }

  // 一次提交多份草稿（把超过 20 个链接的输入拆成若干份）：全部校验通过才写入，任何一份不合格则一条都不保存，
  // 且只落盘一次，不会出现“只保存了前几份”的中间状态。输入框随之清空。
  enqueueMany(requestIds: string[], inputs: string[], mode: CaptureMode): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      if (requestIds.length === 0 || requestIds.length !== inputs.length) throw new Error('拆分保存的参数无效');
      for (let i = 0; i < inputs.length; i++) validateInput(inputs[i]);
      const next = this.snapshot();
      if (next.drafts.length + inputs.length > 100) {
        throw new Error(`本机已有 ${next.drafts.length} 条草稿，再保存 ${inputs.length} 条会超过 100 条上限，请先发送或清理`);
      }
      const now = Date.now();
      for (let i = 0; i < inputs.length; i++) {
        const draft = new Draft();
        draft.requestId = requestIds[i]; draft.input = inputs[i]; draft.mode = mode; draft.createdAt = now;
        next.drafts.push(draft);
      }
      next.editor = '';
      await this.commit(next);
    });
  }

  receive(requestId: string, input: string): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      validateInput(input);
      const next = this.snapshot();
      if (next.drafts.length >= 100) throw new Error('本机草稿已满（100 条），请先发送或清理后再分享');
      const draft = new Draft(); draft.requestId = requestId; draft.input = input; draft.createdAt = Date.now();
      next.drafts.push(draft); // 分享始终另存草稿，不覆盖正在编辑的文字。
      await this.commit(next);
    });
  }

  // 发送成功时返回已校验的接收回执，草稿此时已从本机移除。
  async send(requestId: string, binding: string, sender: CaptureSender): Promise<Receipt> {
    // 同步登记在途状态，避免双击等并发调用同时越过检查。
    if (this.inFlight.has(requestId)) throw new Error('这条内容正在发送，请等待结果');
    this.inFlight.add(requestId);
    try {
      return await this.sendOnce(requestId, binding, sender);
    } finally {
      this.inFlight.delete(requestId);
    }
  }

  private async sendOnce(requestId: string, binding: string, sender: CaptureSender): Promise<Receipt> {
    if (!binding) throw new Error('请先配对电脑');
    // 第一步（串行）：校验并先持久化“已尝试”，之后才允许发出网络请求。
    const start = await this.serial<SendStart>(async (): Promise<SendStart> => {
      const next = this.snapshot();
      const draft = next.drafts.find((item: Draft) => item.requestId === requestId);
      if (!draft) throw new Error('草稿不存在，请刷新');
      if (draft.attempted && draft.binding !== binding) throw new Error('此条曾发往另一台电脑，请先核对原电脑的收集记录');
      const result = new SendStart();
      result.priorAttempted = draft.attempted;
      result.priorBinding = draft.binding;
      draft.attempted = true; draft.binding = binding;
      await this.commit(next); // 必须先持久化，再发出网络请求。
      result.body = { requestId: draft.requestId, input: draft.input, mode: draft.mode };
      return result;
    });
    // 第二步（不占串行链）：网络请求期间其他读改写照常执行。
    let receipt: Receipt;
    try {
      receipt = await sender.send(start.body);
    } catch (error) {
      await this.failed(requestId, error as Error, start);
      throw error as Error;
    }
    if (receipt.requestId !== requestId || !receipt.id || !['accepted', 'received'].includes(receipt.state)) {
      const invalid = new Error('未收到有效的接收回执，草稿已保留');
      await this.failed(requestId, invalid, start); // 普通 Error 归为 uncertain，保留已尝试标记。
      throw invalid;
    }
    // 第三步（串行）：回执有效，在最新状态上移除这条草稿，不影响期间发生的其他改动。
    await this.serial<void>(async (): Promise<void> => {
      const acknowledged = this.snapshot();
      acknowledged.drafts = acknowledged.drafts.filter((item: Draft) => item.requestId !== requestId);
      await this.commit(acknowledged);
    });
    return receipt;
  }

  private failed(requestId: string, error: Error, start: SendStart): Promise<void> {
    return this.serial<void>((): Promise<void> => this.recordFailure(requestId, error, start.priorAttempted, start.priorBinding));
  }

  // 发送失败后按 SendOutcome 修正“已尝试”标记并记录原因。
  // 只有在能证明中转没有保存时才放开修改；落盘失败时保持发送前写入的已尝试状态（偏保守），不掩盖原始错误。
  private async recordFailure(requestId: string, error: Error, priorAttempted: boolean, priorBinding: string): Promise<void> {
    const outcome = classifySendFailure(error);
    const next = this.snapshot();
    const draft = next.drafts.find((item: Draft) => item.requestId === requestId);
    if (!draft) return;
    draft.lastError = failureDetail(error);
    draft.lastErrorAt = Date.now();
    if (outcome === 'rejected') {
      draft.attempted = false; draft.binding = '';
    } else if (outcome === 'notProcessed') {
      draft.attempted = priorAttempted; draft.binding = priorBinding;
    }
    try {
      await this.commit(next);
    } catch (saveError) {
      // 忽略：内存与磁盘仍是“已尝试”的保守状态，重试沿用原编号。
    }
  }

  remove(requestId: string): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      if (this.inFlight.has(requestId)) throw new Error('这条内容正在发送，请等待结果后再删除');
      const next = this.snapshot();
      next.drafts = next.drafts.filter((item: Draft) => item.requestId !== requestId);
      await this.commit(next);
    });
  }

  restoreForEdit(requestId: string): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      if (this.inFlight.has(requestId)) throw new Error('这条内容正在发送，请等待结果后再编辑');
      const next = this.snapshot();
      if (next.editor.trim()) throw new Error('请先保存输入框中的内容，再编辑这条草稿');
      const draft = next.drafts.find((item: Draft) => item.requestId === requestId);
      if (!draft) throw new Error('草稿不存在，请刷新');
      if (draft.attempted) throw new Error('此条已尝试发送，为避免重复或内容冲突，不能修改原文');
      next.editor = draft.input; next.mode = draft.mode;
      next.drafts = next.drafts.filter((item: Draft) => item.requestId !== requestId);
      await this.commit(next);
    });
  }
}
