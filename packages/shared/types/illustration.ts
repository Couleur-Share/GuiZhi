/**
 * 正文配图（AI 文生图）的跨进程类型。
 *
 * 风格预设是数据不是代码：存 `config/illustration-styles.json`
 * （见 packages/core/src/illustration-styles.ts），用户可自行增删改。
 * 流程分两段——文本模型先策划出 shot list，图像模型再逐张生成；
 * 成图以 `local-image://` 引用写进条目正文，删除回收与导出因此自动生效。
 */
import type { KnowledgeItem } from "./knowledge";

export const ILLUSTRATION_ASPECT_RATIOS = ["16:9", "4:3", "1:1"] as const;

export type IllustrationAspectRatio =
  (typeof ILLUSTRATION_ASPECT_RATIOS)[number];

export interface IllustrationStyle {
  id: string;
  /** 选择器里的名字 */
  name: string;
  /** 一句话说明这套风格适合什么内容 */
  description: string;
  /**
   * 选择器里的分组名，自由文本。
   *
   * 刻意不做固定枚举：内容类型会一直长，枚举一定会不够用。留空则排在最前、
   * 不带分组标题。
   */
  group: string;
  /** 画法、配色、留白——拼进生图提示词的风格段 */
  visualDna: string;
  /** 固定出镜的角色描述；留空则不要求画面里有角色 */
  character: string;
  /** 明确排除的观感 */
  negative: string;
  aspectRatio: IllustrationAspectRatio;
  /** 单篇最多配几张 */
  maxShots: number;
  /** 单图最多几处文字标注（越少越不容易出错字） */
  maxLabels: number;
}

/** 一张配图的规格：文本模型策划产出，也是生图提示词的输入 */
export interface IllustrationShot {
  /** 插在这个序号的正文块之后 */
  afterBlock: number;
  /** 图题，同时作为 Markdown 的 alt 文本 */
  topic: string;
  /** 这张图要表达的核心意思 */
  coreIdea: string;
  /** 物理情境类型（分拣 / 衡量取舍 / 过滤漏斗 …），刻意不用图表体裁命名 */
  scene: string;
  /** 具体画面：画面里有什么、谁在做什么动作 */
  composition: string;
  /**
   * 画面里要出现的具体物件，必须取自原文。
   *
   * 交给图像模型自由发挥就会编出与正文冲突的例子——实测里「四象限消费」
   * 那篇的手机被画进了「谨慎购买」象限，而正文明确把它归在「买好的」。
   */
  elements: string[];
  /** 画在图上的中文标注词 */
  labels: string[];
}

/** 已写进正文的一张配图 */
export interface IllustrationEntry {
  assetFileName: string;
  alt: string;
}

export interface IllustrationPlanResult {
  success: boolean;
  /** 未配置文本模型（UI 引导去设置） */
  notConfigured?: boolean;
  error?: string;
  shots?: IllustrationShot[];
  /** 实际使用的风格预设（渲染进程回显选中项） */
  styleId?: string;
  /**
   * 模型读完正文后建议改用的风格。
   *
   * 只在与当前选中的不同、且确实存在于预设列表里时回传。界面上只做建议、
   * 点了才换——用户可能是有意选的这套，悄悄替他改掉最招人烦。
   */
  suggestedStyleId?: string;
}

/** 逐张生成的失败记录：单张失败不阻断其余 */
export interface IllustrationFailure {
  /**
   * 这张图原本要插在哪个段落之后。
   *
   * 面板据此把失败项挑回 shot list 单独重试。序号是**插入之后**的：
   * 同批成功的图会把后面段落整体顶后，不修正就会补到错的位置。
   */
  afterBlock: number;
  topic: string;
  error: string;
}

export interface IllustrationGenerateResult {
  success: boolean;
  /** 未配置 imageGen 路由模型 */
  notConfigured?: boolean;
  error?: string;
  item?: KnowledgeItem;
  /** 成功写入的张数 */
  generated?: number;
  /** 从正文移除的张数（清空全部时回传） */
  removed?: number;
  failures?: IllustrationFailure[];
}

/** 主进程 → 渲染进程的逐张进度 */
export interface IllustrationProgress {
  itemId: string;
  /** 从 1 开始 */
  index: number;
  total: number;
  topic: string;
  phase: "generating" | "done" | "failed";
  error?: string;
}
