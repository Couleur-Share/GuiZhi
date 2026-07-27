import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  AIProtocol,
  IllustrationGenerateResult,
  IllustrationPlanResult,
  IllustrationProgress,
  IllustrationShot,
  IllustrationStyle,
} from "@guizhi/shared/types";

export interface IllustrationStylesWriteResult {
  success: boolean;
  error?: string;
  styles?: IllustrationStyle[];
}

export interface RevealStylesFileResult {
  success: boolean;
  error?: string;
}

export interface IllustrationTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

export const illustrationApi = {
  /** 可用的配图风格预设（config/illustration-styles.json） */
  styles: (): Promise<IllustrationStyle[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_STYLES),
  /** 应用内编辑器回写整份预设列表；返回落盘后的规范化结果 */
  saveStyles: (
    styles: IllustrationStyle[],
  ): Promise<IllustrationStylesWriteResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_SAVE_STYLES, { styles }),
  /** 内置预设；编辑器「恢复内置预设」只改本地草稿，保存才落盘 */
  builtInStyles: (): Promise<IllustrationStyle[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_BUILT_IN_STYLES),
  /** 在文件管理器里定位预设文件（留给要手改 JSON 的用户） */
  revealStylesFile: (): Promise<RevealStylesFileResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_REVEAL_STYLES_FILE),
  /**
   * 文生图模型连通性测试。
   * 不能复用 chat completions 那条测试路径——文生图模型会回 model_not_supported。
   */
  testModel: (config: {
    apiUrl: string;
    apiKey: string;
    model: string;
    apiProtocol?: AIProtocol;
    provider?: string;
  }): Promise<IllustrationTestResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_TEST, config),
  /**
   * 只出配图规格，不生成图片。
   * shotCount 省略或为 0 表示「自动」——按可配图段落数推一个稳定的张数。
   */
  plan: (
    itemId: string,
    styleId: string,
    shotCount?: number,
  ): Promise<IllustrationPlanResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_PLAN, {
      itemId,
      styleId,
      shotCount,
    }),
  /** 按规格逐张生成并写入正文 */
  generate: (
    itemId: string,
    styleId: string,
    shots: IllustrationShot[],
  ): Promise<IllustrationGenerateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_GENERATE, {
      itemId,
      styleId,
      shots,
    }),
  /** 重新生成正文里已有的某一张（原位替换） */
  regenerate: (
    itemId: string,
    styleId: string,
    assetFileName: string,
  ): Promise<IllustrationGenerateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_REGENERATE, {
      itemId,
      styleId,
      assetFileName,
    }),
  remove: (
    itemId: string,
    assetFileName: string,
  ): Promise<IllustrationGenerateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_REMOVE, {
      itemId,
      assetFileName,
    }),
  /** 一次移除正文里的全部配图，磁盘资产随之回收 */
  clear: (itemId: string): Promise<IllustrationGenerateResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.ILLUSTRATION_CLEAR, { itemId }),
  /** 中断该条目在途的策划或生成 */
  cancel: (itemId: string): void => {
    ipcRenderer.send(IPC_CHANNELS.ILLUSTRATION_CANCEL, itemId);
  },
  /** 订阅逐张生成进度，返回退订函数 */
  onProgress: (
    callback: (progress: IllustrationProgress) => void,
  ): (() => void) => {
    const listener = (_event: unknown, progress: IllustrationProgress): void =>
      callback(progress);
    ipcRenderer.on(IPC_CHANNELS.ILLUSTRATION_PROGRESS, listener);
    return () => {
      ipcRenderer.removeListener(IPC_CHANNELS.ILLUSTRATION_PROGRESS, listener);
    };
  },
};
