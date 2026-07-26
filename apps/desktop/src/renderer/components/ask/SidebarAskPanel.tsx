import { useEffect, useState } from "react";
import { Loader2Icon, MessageSquarePlusIcon, RefreshCwIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AskSessionMeta } from "@guizhi/shared/types";
import { useAskStore } from "../../stores/ask.store";
import { useSemanticStore } from "../../stores/semantic.store";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { useToast } from "../ui/Toast";
import { formatItemTime } from "../library/type-meta";

/** 语义索引状态卡：仅在 embedding 模型配置后出现 */
function SemanticIndexCard() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const status = useSemanticStore((state) => state.status);
  const isConfigured = useSemanticStore((state) => state.isConfigured);
  const isIndexing = useSemanticStore((state) => state.isIndexing);
  const indexedThisRun = useSemanticStore((state) => state.indexedThisRun);
  const notice = useSemanticStore((state) => state.notice);
  const consumeNotice = useSemanticStore((state) => state.consumeNotice);
  const refreshStatus = useSemanticStore((state) => state.refreshStatus);
  const runIndexing = useSemanticStore((state) => state.runIndexing);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  // 手动触发的那一轮必须有回执：失败、一条没跑成，此前都只进 console
  useEffect(() => {
    if (!notice) {
      return;
    }
    // 取走后 store 里就没有了：StrictMode 下 effect 会带着同一份闭包再跑一次，
    // 若在这里重读 notice 就会把同一条回执弹两遍
    const pending = consumeNotice();
    if (!pending) {
      return;
    }
    if (pending.kind === "done") {
      showToast(
        t("ask.semanticDone", "已索引 {{count}} 条", { count: pending.indexed }),
        "success",
      );
    } else if (pending.kind === "partial") {
      showToast(
        t("ask.semanticPartial", "索引 {{indexed}} 条，{{failed}} 条失败：{{message}}", {
          indexed: pending.indexed,
          failed: pending.failed,
          message: pending.message ?? "",
        }),
        "error",
      );
    } else if (pending.kind === "failed") {
      showToast(
        t("ask.semanticFailed", "索引失败：{{message}}", {
          message: pending.message ?? t("common.unknownError", "未知错误"),
        }),
        "error",
      );
    } else {
      showToast(t("ask.semanticNothing", "没有需要索引的内容"), "info");
    }
  }, [notice, consumeNotice, showToast, t]);

  if (!isConfigured || !status) {
    return null;
  }

  const pending = Math.max(0, status.eligibleItems - status.indexedItems);
  return (
    <div className="mt-2 rounded-lg border border-sidebar-border/70 px-3 py-2">
      <p className="text-xs text-sidebar-foreground/60">
        {t("ask.semanticStatus", "语义索引 {{indexed}}/{{eligible}}", {
          indexed: status.indexedItems,
          eligible: status.eligibleItems,
        })}
      </p>
      {pending > 0 || isIndexing ? (
        <button
          type="button"
          onClick={() => void runIndexing()}
          disabled={isIndexing}
          className="mt-1.5 flex w-full items-center justify-center gap-1.5 rounded-lg border border-sidebar-border px-3 py-1.5 text-xs text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground disabled:opacity-60"
        >
          {isIndexing ? (
            <>
              <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
              {t("ask.semanticIndexing", "索引中…（已完成 {{count}}）", {
                count: indexedThisRun,
              })}
            </>
          ) : (
            <>
              <RefreshCwIcon className="h-3.5 w-3.5" aria-hidden="true" />
              {t("ask.semanticRun", "索引 {{count}} 条新内容", {
                count: pending,
              })}
            </>
          )}
        </button>
      ) : null}
    </div>
  );
}

/**
 * 问答模块侧栏：新对话入口 + 历史会话列表（切换 / 删除）。
 */
export function SidebarAskPanel() {
  const { t } = useTranslation();
  const sessions = useAskStore((state) => state.sessions);
  const activeSessionId = useAskStore((state) => state.activeSessionId);
  const initialize = useAskStore((state) => state.initialize);
  const newSession = useAskStore((state) => state.newSession);
  const switchSession = useAskStore((state) => state.switchSession);
  const deleteSession = useAskStore((state) => state.deleteSession);

  const [confirmDelete, setConfirmDelete] = useState<AskSessionMeta | null>(
    null,
  );

  useEffect(() => {
    void initialize();
  }, [initialize]);

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3">
      <div className="pt-3">
        <button
          type="button"
          onClick={newSession}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <MessageSquarePlusIcon className="h-4 w-4" aria-hidden="true" />
          {t("ask.newSession", "新对话")}
        </button>
        <SemanticIndexCard />
      </div>

      <div className="flex items-center gap-2 px-3 pb-1 pt-4">
        <span className="text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/50">
          {t("ask.sessionList", "历史会话")}
        </span>
        <div className="h-px flex-1 bg-sidebar-border/60" />
      </div>

      {sessions.length === 0 ? (
        <p className="px-3 py-1 text-xs text-sidebar-foreground/40">
          {t("ask.sessionListEmpty", "提问后会话会保存在这里")}
        </p>
      ) : (
        sessions.map((session) => {
          const isActive = session.id === activeSessionId;
          return (
            <div key={session.id} className="group relative w-full py-0.5">
              <button
                type="button"
                onClick={() => void switchSession(session.id)}
                className={`flex w-full flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left transition-all duration-smooth ${
                  isActive
                    ? "bg-primary text-white shadow-sm"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                <span className="w-full truncate pr-5 text-sm">
                  {session.title || t("ask.untitledSession", "新对话")}
                </span>
                <span
                  className={`text-[10px] ${
                    isActive ? "text-white/70" : "text-sidebar-foreground/40"
                  }`}
                >
                  {formatItemTime(session.updatedAt)}
                </span>
              </button>
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  setConfirmDelete(session);
                }}
                title={t("ask.deleteSession", "删除会话")}
                aria-label={t("ask.deleteSession", "删除会话")}
                className={`absolute right-1.5 top-2 hidden h-5 w-5 items-center justify-center rounded transition-colors group-hover:flex ${
                  isActive
                    ? "text-white/70 hover:bg-white/20 hover:text-white"
                    : "text-sidebar-foreground/40 hover:bg-sidebar-accent hover:text-destructive"
                }`}
              >
                <XIcon className="h-3.5 w-3.5" aria-hidden="true" />
              </button>
            </div>
          );
        })
      )}

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={() => {
          if (confirmDelete) {
            void deleteSession(confirmDelete.id);
          }
          setConfirmDelete(null);
        }}
        title={t("ask.deleteSession", "删除会话")}
        message={t(
          "ask.deleteSessionConfirm",
          "删除会话「{{title}}」？聊天记录将被永久移除。",
          { title: confirmDelete?.title ?? "" },
        )}
        confirmText={t("common.confirm", "确认")}
        cancelText={t("common.cancel", "取消")}
        variant="destructive"
      />
    </div>
  );
}
