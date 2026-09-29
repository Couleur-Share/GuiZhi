// 延迟删除：删除时先把草稿从列表里“藏起来”，撤销窗口（8 秒）结束才真正删除。
// 窗口期间草稿仍保存在本机：进程被系统回收时它们原样保留（偏安全的方向），所以不需要软删除标记或额外的存储字段。
export const DELETE_UNDO_MS: number = 8000;

export class DeleteFailure {
  requestId: string = '';
  message: string = '';
}

// 真正删除的能力：逐条删除并返回失败的条目。实现方应自行捕获单条错误；即使整体抛出，这里也会把整批记为失败。
export type DeleteCommit = (ids: string[]) => Promise<DeleteFailure[]>;

export class DeleteHooks {
  // 隐藏集合或撤销窗口发生变化：hidden 是当前应从界面隐藏的编号，undoable 表示撤销窗口是否仍开着。
  changed: (hidden: string[], undoable: boolean) => void = (hidden: string[], undoable: boolean): void => {};
  // 有条目没能删除：这些条目已经重新出现在列表里，界面应说明原因（逐条分别记账，不因一条失败就整批报成功）。
  failed: (failures: DeleteFailure[]) => void = (failures: DeleteFailure[]): void => {};
}

export class PendingDeletes {
  private commit: DeleteCommit;
  private hooks: DeleteHooks;
  private setTimer: (action: () => void, delay: number) => number;
  private clearTimer: (id: number) => void;
  // 撤销窗口内的编号；窗口同一时刻只有一个（提示条只有一个）。
  private waiting: string[] = [];
  // 窗口已结束、正在真正删除的编号：删除完成前仍要保持隐藏，避免列表闪回。
  private committing: string[] = [];
  private timer: number = -1;

  constructor(commit: DeleteCommit, hooks: DeleteHooks,
    setTimer: (action: () => void, delay: number) => number, clearTimer: (id: number) => void) {
    this.commit = commit; this.hooks = hooks; this.setTimer = setTimer; this.clearTimer = clearTimer;
  }

  hidden(): string[] { return this.waiting.concat(this.committing); }

  undoable(): boolean { return this.waiting.length > 0; }

  private announce(): void { this.hooks.changed(this.hidden(), this.undoable()); }

  // 取走撤销窗口里的编号并取消计时器；不通知，由调用方在整理好状态后统一通知。
  private takeWaiting(): string[] {
    if (this.timer !== -1) { this.clearTimer(this.timer); this.timer = -1; }
    const ids = this.waiting;
    this.waiting = [];
    return ids;
  }

  private async finish(ids: string[]): Promise<void> {
    let failures: DeleteFailure[] = [];
    try {
      failures = await this.commit(ids);
    } catch (error) {
      const message = (error as Error).message || '删除失败';
      for (let i = 0; i < ids.length; i++) {
        const failure = new DeleteFailure();
        failure.requestId = ids[i]; failure.message = message;
        failures.push(failure);
      }
    }
    this.committing = this.committing.filter((id: string): boolean => !ids.includes(id));
    this.announce();
    if (failures.length > 0) this.hooks.failed(failures);
  }

  // 开始一批新的删除。上一批的撤销窗口在此结束并立即真正删除（同一时刻只保留一个可撤销的批次）。
  schedule(ids: string[]): Promise<void> {
    if (ids.length === 0) return Promise.resolve();
    const previous = this.takeWaiting();
    this.committing = this.committing.concat(previous);
    this.waiting = ids.slice();
    this.timer = this.setTimer((): void => { this.flush().catch((): void => {}); }, DELETE_UNDO_MS);
    this.announce();
    return previous.length > 0 ? this.finish(previous) : Promise.resolve();
  }

  // 撤销窗口内撤销：返回被恢复的编号；窗口已结束或没有待删除项时返回空数组。
  undo(): string[] {
    const ids = this.takeWaiting();
    if (ids.length > 0) this.announce();
    return ids;
  }

  // 立即结束撤销窗口并真正删除（计时到点、应用进入后台时调用）。
  flush(): Promise<void> {
    const ids = this.takeWaiting();
    if (ids.length === 0) return Promise.resolve();
    this.committing = this.committing.concat(ids);
    this.announce();
    return this.finish(ids);
  }
}
