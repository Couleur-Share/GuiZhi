/**
 * 文字稿 AI 排版的进度上报类型。
 * 排版按块串行请求模型，长稿可达数十块、几分钟，按钮不能只是转圈。
 */

export interface TranscriptFormatProgress {
  itemId: string;
  /** 当前块序号，从 1 开始 */
  current: number;
  total: number;
}

/**
 * 重新生成文字稿的三个阶段。三步都以分钟计，界面必须说清正在哪一步——
 * 只报「正在转写」的话，后两步期间看起来就是卡住了。
 */
export type TranscribeStage = "transcribing" | "formatting" | "summarizing";

/**
 * 转写链路进行中的状态。
 *
 * 刻意没有百分比：funasr 在 VAD 路径下给不出可用的分母，写死的分母只会骗人
 * （与导入阶段不给「第 N 步 / 共 M 步」是同一条理由）。真正要回答的问题是
 * 「它还在动吗」，靠 stalledMs 说话。
 */
export interface TranscribeProgress {
  itemId: string;
  stage: TranscribeStage;
  elapsedMs: number;
  /** 距上次心跳多久；只有转写阶段的本地引擎给得出，其余为 undefined */
  stalledMs?: number;
}

/**
 * 当前「语音转写」路由支持的可选能力。
 * 界面据此决定要不要摆出入口——摆一个点了必然报错的按钮不如不摆。
 */
export interface MediaCapabilities {
  /** 区分说话人，仅内置本地转写引擎支持 */
  diarization: boolean;
}
