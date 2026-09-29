import { CaptureMode } from './Protocol';

// 输入框内容的防抖落盘：每次按键都整包写入 Preferences 会随草稿增多而变慢，
// 这里合并连续输入，停顿 delay 毫秒后只写一次；关闭、保存、进入后台前调用 flush() 立即写出。
// 计时器与写入函数均由外部注入，同一份源码可在 Node 测试与 ArkTS 中运行。
export type AutosaveWrite = (input: string, mode: CaptureMode) => Promise<void>;
export type AutosaveSetTimer = (action: () => void, delay: number) => number;
export type AutosaveClearTimer = (id: number) => void;
// saving 为 true 表示有尚未落盘的输入；error 非空时是可展示、可复制的失败原因。
export type AutosaveReport = (saving: boolean, error: string) => void;

export class EditorAutosave {
  private write: AutosaveWrite;
  private setTimer: AutosaveSetTimer;
  private clearTimer: AutosaveClearTimer;
  private report: AutosaveReport;
  private delay: number;
  private timer: number = -1;
  private dirty: boolean = false;
  private input: string = '';
  private mode: CaptureMode = 'auto';
  // 所有写入串行排队：后一次写入一定发生在前一次结束之后，落盘顺序与输入顺序一致。
  private chain: Promise<void> = Promise.resolve();

  constructor(write: AutosaveWrite, setTimer: AutosaveSetTimer, clearTimer: AutosaveClearTimer,
    report: AutosaveReport, delay: number = 400) {
    this.write = write; this.setTimer = setTimer; this.clearTimer = clearTimer; this.report = report; this.delay = delay;
  }

  // 记录最新输入并重新计时；从最后一次输入起停顿 delay 毫秒才落盘。
  schedule(input: string, mode: CaptureMode): void {
    this.input = input; this.mode = mode; this.dirty = true;
    this.report(true, '');
    this.stopTimer();
    this.timer = this.setTimer(() => {
      this.timer = -1;
      // 失败原因已通过 report 展示，dirty 保留，之后的 flush 会重试；这里不再向外抛。
      this.flush().catch(() => {});
    }, this.delay);
  }

  // 立即写出最新输入并等待完成；写入失败时抛出原始错误，调用者据此决定是否收起编辑面板。
  flush(): Promise<void> {
    this.stopTimer();
    const run = this.chain.then(() => this.writeLatest());
    this.chain = run.catch(() => {});
    return run;
  }

  private stopTimer(): void {
    if (this.timer !== -1) { this.clearTimer(this.timer); this.timer = -1; }
  }

  private async writeLatest(): Promise<void> {
    if (!this.dirty) return;
    const input = this.input;
    const mode = this.mode;
    try {
      await this.write(input, mode);
    } catch (error) {
      const reason = (error as Error).message ? (error as Error).message.substring(0, 200) : '';
      this.report(false, reason ? `自动保存失败：${reason}。请保留输入内容并点击保存重试` : '自动保存失败，请保留输入内容并点击保存重试');
      throw error as Error;
    }
    // 写入期间又有新输入：保持“待保存”，由新的计时器负责落盘。
    if (this.input === input && this.mode === mode) {
      this.dirty = false;
      this.report(false, '');
    }
  }
}
