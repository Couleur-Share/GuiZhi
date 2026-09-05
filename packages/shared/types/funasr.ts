/**
 * 本地转写引擎（托管 funasr-server + SenseVoiceSmall）类型。
 * 安装 / 卸载 / 状态由主进程管理，渲染进程仅展示与触发。
 */

export type FunasrInstallPhase = "runtime" | "deps" | "models" | "prepare" | "backup" | "verify" | "rollback";

/** python = Windows 全量；gguf = macOS arm64 轻量 */
export type FunasrInstallFlavor = "python" | "gguf";

export interface FunasrStatus {
  installed: boolean;
  /** 服务当前是否在运行（按需启动，未运行不代表异常） */
  running: boolean;
  /** 服务监听端口（仅绑定 127.0.0.1） */
  port: number;
  /** 托管安装目录（未安装也返回，供 UI 展示） */
  dir: string;
  /** funasr / GGUF 运行时版本（已安装时） */
  version?: string;
  /**
   * 当前平台是否提供应用内一键安装。
   * Windows 与 macOS Apple Silicon；其余平台请配置云端 audioText。
   */
  installSupported: boolean;
  /** 独立更新目前仅支持 Windows Python 引擎 */
  updateSupported?: boolean;
  /** 本机将装 / 已装的引擎形态（决定磁盘占用文案） */
  installFlavor?: FunasrInstallFlavor;
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
  /** 更新完成但有保留备份或清理失败，界面需展示原因和目录。 */
  warning?: string;
}

export interface FunasrOperationResult {
  success: boolean;
  error?: string;
}
