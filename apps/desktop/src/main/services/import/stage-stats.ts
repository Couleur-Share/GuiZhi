/**
 * 导入任务的阶段统计：每个阶段花了多久、发了几次 AI 调用、烧了多少 token。
 *
 * 存在的理由是一次真实的漏网——文字稿排版因为模型默认开着思考，单块要跑
 * 一分多钟、偶尔连撞两次 240 秒超时，一次导入被它占掉 79% 的时间；而终态
 * 任务在界面上不留任何耗时痕迹，这件事只能靠翻数据库和 grep 日志才发现。
 *
 * 只记事实，不判「快慢」：什么叫慢取决于模型、网络与内容长度，写死一个阈值
 * 在慢渠道上会天天误报（与「不给第 N 步 / 共 M 步」「转写只报已用不报百分比」
 * 是同一条理由）。8:13 这个数字摆在行上，人自己看得出不对。
 */
import type { ImportStage, ImportStageStat } from "@guizhi/shared/types";
import type { AiCallRecord } from "../ai-call-context";

export class ImportStageStatsRecorder {
  private readonly stats: ImportStageStat[] = [];
  private current: ImportStageStat | null = null;
  private enteredAt: number;

  constructor(private readonly now: () => number = Date.now) {
    this.enteredAt = this.now();
  }

  /**
   * 切换到新阶段（传 null 表示任务结束），并结算上一个阶段的耗时。
   *
   * 同一阶段被再次进入时累加到原有那条而不是新开一条：阶段名是用户认得的
   * 标签，列表里出现两行「文字稿排版」只会让人以为是 bug。
   */
  transition(stage: ImportStage | null): void {
    const at = this.now();
    if (this.current) {
      this.current.ms += Math.max(0, at - this.enteredAt);
    }
    this.enteredAt = at;
    if (!stage) {
      this.current = null;
      return;
    }
    const existing = this.stats.find((entry) => entry.stage === stage);
    if (existing) {
      this.current = existing;
      return;
    }
    const created: ImportStageStat = { stage, ms: 0 };
    this.stats.push(created);
    this.current = created;
  }

  /** 把一次 AI 调用记到当前阶段上；阶段之外发生的调用无处可归，丢弃 */
  recordAiCall(record: AiCallRecord): void {
    const current = this.current;
    if (!current) {
      return;
    }
    current.calls = (current.calls ?? 0) + 1;
    if (record.failed) {
      current.failedCalls = (current.failedCalls ?? 0) + 1;
    }
    if (record.promptTokens) {
      current.promptTokens = (current.promptTokens ?? 0) + record.promptTokens;
    }
    if (record.completionTokens) {
      current.completionTokens =
        (current.completionTokens ?? 0) + record.completionTokens;
    }
    if (record.model && !current.models?.includes(record.model)) {
      current.models = [...(current.models ?? []), record.model];
    }
  }

  /**
   * 当前快照，含正在进行中那个阶段的已用时长。
   *
   * 深拷贝：调用方会把它序列化进数据库，而记录器还在继续累加，
   * 交出内部对象等于把两边的状态绑在一起。
   */
  snapshot(): ImportStageStat[] {
    const pendingMs = this.current ? Math.max(0, this.now() - this.enteredAt) : 0;
    return this.stats.map((entry) => ({
      ...entry,
      ms: entry === this.current ? entry.ms + pendingMs : entry.ms,
      ...(entry.models ? { models: [...entry.models] } : {}),
    }));
  }
}