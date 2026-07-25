/**
 * 本地转写引擎（托管 funasr-server + SenseVoiceSmall）类型。
 * 安装 / 卸载 / 状态由主进程管理，渲染进程仅展示与触发。
 */

export type FunasrInstallPhase = "runtime" | "deps" | "models";

export interface FunasrStatus {
  installed: boolean;
  /** 服务当前是否在运行（按需启动，未运行不代表异常） */
  running: boolean;
  /** 服务监听端口（仅绑定 127.0.0.1） */
  port: number;
  /** 托管安装目录（未安装也返回，供 UI 展示） */
  dir: string;
  /** funasr 包版本（已安装时） */
  version?: string;
}

export interface FunasrInstallProgress {
  phase: FunasrInstallPhase;
  /** 阶段内进度 0-100；无法计算时为 null（按阶段文案展示） */
  percent: number | null;
  /** 附加说明（下载量 / pip 输出摘要等） */
  detail?: string;
}

export interface FunasrInstallResult {
  success: boolean;
  version?: string;
  error?: string;
}

export interface FunasrOperationResult {
  success: boolean;
  error?: string;
}
