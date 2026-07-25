import { create } from "zustand";
import type { UpdateStatus } from "../components/UpdateDialog";

/**
 * 最近一次更新检查的结果（仅本次会话，不持久化）。
 *
 * 存在的意义是让「检查过了」这件事在界面上留下痕迹：自动检查在没有新版本或
 * 失败时不会改变任何 UI，关于页据此显示「上次检查 …」，否则用户无从判断
 * 自动检查究竟跑没跑。
 */
export type UpdateCheckOutcome = "available" | "not-available" | "error";

interface UpdaterStoreState {
  lastCheckAt: number | null;
  lastCheckOutcome: UpdateCheckOutcome | null;
  lastCheckVersion: string | null;
  lastCheckError: string | null;
  /** 记录主进程推送的状态事件（checking / downloading / downloaded 不算检查结果） */
  recordStatus: (status: UpdateStatus) => void;
  /** 记录未产生状态事件的失败：预览通道无可用版本、镜像源全挂等 */
  recordFailure: (error: string) => void;
}

export const useUpdaterStore = create<UpdaterStoreState>()((set) => ({
  lastCheckAt: null,
  lastCheckOutcome: null,
  lastCheckVersion: null,
  lastCheckError: null,
  recordStatus: (status) => {
    if (status.status === "available" || status.status === "not-available") {
      set({
        lastCheckAt: Date.now(),
        lastCheckOutcome: status.status,
        lastCheckVersion: status.info?.version ?? null,
        lastCheckError: null,
      });
      return;
    }
    if (status.status === "error") {
      set({
        lastCheckAt: Date.now(),
        lastCheckOutcome: "error",
        lastCheckVersion: null,
        lastCheckError: status.error,
      });
    }
  },
  recordFailure: (error) =>
    set({
      lastCheckAt: Date.now(),
      lastCheckOutcome: "error",
      lastCheckVersion: null,
      lastCheckError: error,
    }),
}));
