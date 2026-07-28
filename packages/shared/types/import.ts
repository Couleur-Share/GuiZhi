/**
 * 采集 / 导入管线类型。
 */
import type { KnowledgeItemType } from "./knowledge";

export const IMPORT_SOURCE_KINDS = ["text", "file", "url"] as const;

export type ImportSourceKind = (typeof IMPORT_SOURCE_KINDS)[number];

export const IMPORT_TASK_STATUSES = [
  "pending",
  "processing",
  "completed",
  "failed",
  "canceled",
  "duplicate",
] as const;

export type ImportTaskStatus = (typeof IMPORT_TASK_STATUSES)[number];

/**
 * 导入子阶段。
 *
 * 视频链路会串起元数据解析、音频下载、转码、转写、排版、总结六步，
 * 最长可达几十分钟；此前它们全部落在 fetching 一个状态里，界面从头到尾
 * 只显示「抓取中」，用户无从判断是在正常工作还是卡死了。
 */
export const IMPORT_STAGES = [
  "fetching",
  "extracting",
  "saving",
  // ── 在线视频子阶段 ──
  "video-metadata",
  "video-audio",
  "transcoding",
  "transcribing",
  "formatting",
  "summarizing",
  // ── 图文子阶段 ──
  "image-download",
  "image-ocr",
  // ── 论坛帖子子阶段 ──
  "forum-replies",
] as const;

export type ImportStage = (typeof IMPORT_STAGES)[number];

/**
 * 单个阶段的实际开销。任务跑完后留在行上，用来回答「这次为什么这么久」。
 *
 * 记的是**结果**而不是请求参数：一次 1600 字的文字稿排版，输出 1570 字却烧掉
 * 8271 个 completion token，这个比值自明地指出模型在闷头写思维链；而记一栏
 * 「思考：已关闭」只说明我们发了什么，说明不了模型做了什么（实测有模型收下
 * 关闭参数照样思考）。参数是上一个缺陷的形状，耗时与 token 才是通用的。
 */
export interface ImportStageStat {
  stage: ImportStage;
  /** 该阶段累计耗时（毫秒）。同一阶段被再次进入时累加，不新开一条 */
  ms: number;
  /** 该阶段发起的 AI 调用次数，含失败的（超时与限流同样可能计费） */
  calls?: number;
  /** 其中失败的次数 */
  failedCalls?: number;
  promptTokens?: number;
  completionTokens?: number;
  /** 该阶段用到的模型，按首次出现排序 */
  models?: string[];
}

export interface ImportTask {
  id: string;
  sourceKind: ImportSourceKind;
  /** 原始输入：文本内容 / 文件绝对路径 / URL */
  sourceInput: string;
  /** 列表显示名（文本首行 / 文件名 / URL） */
  displayName: string;
  status: ImportTaskStatus;
  stage?: ImportStage | null;
  error?: string | null;
  /**
   * 入库了、但内容有缺失的原因（转写失败 / 未配置转写模型…）。
   *
   * 与 error 是两回事：error 意味着任务没做成、条目不在库里，重试是唯一出路；
   * warning 的条目已经在库里，只是少了最值钱的那部分。此前这类降级只写进正文
   * 的一行注记，列表上照旧是绿色的「已完成」——用户点开才发现没有文字稿，
   * 而一批三十条里漏掉哪条根本无从判断。
   */
  warning?: string | null;
  /**
   * 抽取出的条目类型（视频 / 论坛 / 图片…），抽取成功后回写。
   *
   * 列表靠它给出与知识库一致的类型图标；抽取完成前为 null，
   * 此时只能按 sourceKind 显示地球 / 文件图标。
   */
  itemType?: KnowledgeItemType | null;
  /** 成功入库的条目 id */
  resultItemId?: string | null;
  /** 去重命中的已有条目 id */
  duplicateItemId?: string | null;
  collectionId?: string | null;
  /** 入库时要打上的标签（采集弹窗里选的） */
  tagNames?: string[];
  /**
   * 各阶段的耗时与 AI 开销，按进入顺序排列；任务跑完前是已完成的那部分。
   *
   * 此前终态任务在界面上不留任何耗时痕迹（`ProgressHint` 只在运行时渲染），
   * 一次「排版花了 8 分钟」的异常要靠翻数据库和日志才看得出来，而用户的
   * 反馈恰恰是「在使用时没发现」。重试会清空它，否则两轮的耗时会叠在一起。
   */
  stageStats?: ImportStageStat[] | null;
  createdAt: number;
  updatedAt: number;
}

export interface EnqueueImportInput {
  kind: ImportSourceKind;
  /** 文本内容 / 文件绝对路径 / URL */
  input: string;
  collectionId?: string | null;
  /**
   * 入库时直接打上的标签。
   *
   * 采集完再回列表里找出来逐条补标签，是整理流程里最费手的一步；
   * 采集当下就知道这批内容属于什么，让它在这里落地。
   */
  tagNames?: string[];
  /** 跳过去重强制创建副本（「仍要创建副本」） */
  forceDuplicate?: boolean;
}
