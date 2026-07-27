import { create } from "zustand";

/**
 * 变更类操作的失败通道。
 *
 * store 里的变更方法（批量打标签、移到回收站、入队…）几乎全被 `void mutate(...)`
 * 调用，抛出去就是一条无人处理的 rejection：界面毫无反应，用户以为改成功了。
 * 让 store 把失败投到这里，由挂在应用根部的 useOperationErrorToast 统一提示——
 * 这样不用给几十个调用点逐个加 try/catch，也不会漏。
 *
 * 只存 i18n key 不存译文：语言可以在运行时切换，翻译要留到渲染那一刻做。
 */
export interface OperationError {
  /** 单调递增；同一个错误连续发生两次也要各提示一次 */
  id: number;
  /** 动作名的 i18n key，如 "library.actionBulkUpdate" */
  actionKey: string;
  /** 动作名兜底文案 */
  actionFallback: string;
  /** 原始报错，进 toast 的「查看详情」 */
  message: string;
}

interface OperationErrorState {
  latest: OperationError | null;
  report: (actionKey: string, actionFallback: string, error: unknown) => void;
  clear: () => void;
}

let sequence = 0;

export const useOperationErrorStore = create<OperationErrorState>()((set) => ({
  latest: null,
  report: (actionKey, actionFallback, error) => {
    sequence += 1;
    const message = error instanceof Error ? error.message : String(error);
    console.error(`[operation] ${actionFallback}失败:`, error);
    // toast 关掉就没了，console 又只进 DevTools；留一份到 logs/error.log
    window.api?.log?.appError({
      scope: "renderer",
      action: actionFallback,
      message,
    });
    set({ latest: { id: sequence, actionKey, actionFallback, message } });
  },
  clear: () => set({ latest: null }),
}));

/** 供 store 内部调用（zustand 之外的模块作用域里拿不到 hook） */
export function reportOperationError(
  actionKey: string,
  actionFallback: string,
  error: unknown,
): void {
  useOperationErrorStore.getState().report(actionKey, actionFallback, error);
}

/**
 * 包住一次变更操作：失败时投进错误通道并返回 false，绝不向外抛。
 *
 * 返回值给「成功后还要弹提示」的调用方用（比如移到回收站的撤销提示），
 * 其余 `void` 调用点忽略即可，错误提示照样会出来。
 */
export async function runGuardedMutation(
  actionKey: string,
  actionFallback: string,
  run: () => Promise<void>,
): Promise<boolean> {
  try {
    await run();
    return true;
  } catch (error) {
    reportOperationError(actionKey, actionFallback, error);
    return false;
  }
}
