import { Draft, LocalState, CaptureMode, Receipt, Submission, validateInput } from './Protocol';

export interface StateStore { save(state: LocalState): Promise<void> }
export interface CaptureSender { send(body: Submission): Promise<Receipt> }

// 所有读改写串行；只有落盘成功才更新内存，网络响应丢失时保留编号与原文。
export class DraftQueue {
  private state: LocalState;
  private store: StateStore;
  private tail: Promise<void> = Promise.resolve();
  constructor(store: StateStore, state: LocalState) { this.store = store; this.state = state; }

  snapshot(): LocalState { return JSON.parse(JSON.stringify(this.state)) as LocalState; }

  private serial(action: () => Promise<void>): Promise<void> {
    const next = this.tail.then(action);
    this.tail = next.catch(() => {}); // 调用者接收失败；后续操作仍可重试。
    return next;
  }

  private async commit(next: LocalState): Promise<void> {
    await this.store.save(next);
    this.state = next;
  }

  edit(input: string, mode: CaptureMode): Promise<void> {
    return this.serial(async () => {
      const next = this.snapshot(); next.editor = input; next.mode = mode;
      await this.commit(next);
    });
  }

  enqueue(requestId: string): Promise<void> {
    return this.serial(async () => {
      validateInput(this.state.editor);
      const next = this.snapshot();
      if (next.drafts.length >= 100) throw new Error('本机已有 100 条草稿，请先发送或清理');
      const draft = new Draft();
      draft.requestId = requestId; draft.input = next.editor; draft.mode = next.mode; draft.createdAt = Date.now();
      next.drafts.push(draft); next.editor = '';
      await this.commit(next);
    });
  }

  receive(requestId: string, input: string): Promise<void> {
    return this.serial(async () => {
      validateInput(input);
      const next = this.snapshot();
      if (next.drafts.length >= 100) throw new Error('本机草稿已满，本次分享尚未保存，请先清理再分享');
      const draft = new Draft(); draft.requestId = requestId; draft.input = input; draft.createdAt = Date.now();
      next.drafts.push(draft); // 分享始终另存草稿，不覆盖正在编辑的文字。
      await this.commit(next);
    });
  }

  send(requestId: string, binding: string, sender: CaptureSender): Promise<void> {
    return this.serial(async () => {
      if (!binding) throw new Error('请先配对电脑');
      const next = this.snapshot();
      const draft = next.drafts.find((item: Draft) => item.requestId === requestId);
      if (!draft) throw new Error('草稿不存在，请刷新');
      if (draft.attempted && draft.binding !== binding) throw new Error('此条曾发往另一台电脑，请先核对原电脑的收集记录');
      draft.attempted = true; draft.binding = binding;
      await this.commit(next); // 必须先持久化，再发出网络请求。
      const body: Submission = { requestId: draft.requestId, input: draft.input, mode: draft.mode };
      const receipt = await sender.send(body);
      if (receipt.requestId !== requestId || !receipt.id || !['accepted', 'received'].includes(receipt.state)) {
        throw new Error('未收到有效的接收回执，草稿已保留');
      }
      const acknowledged = this.snapshot();
      acknowledged.drafts = acknowledged.drafts.filter((item: Draft) => item.requestId !== requestId);
      await this.commit(acknowledged);
    });
  }

  remove(requestId: string): Promise<void> {
    return this.serial(async () => {
      const next = this.snapshot();
      next.drafts = next.drafts.filter((item: Draft) => item.requestId !== requestId);
      await this.commit(next);
    });
  }

  restoreForEdit(requestId: string): Promise<void> {
    return this.serial(async () => {
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
