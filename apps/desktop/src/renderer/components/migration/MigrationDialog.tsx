import { useState } from "react";
import {
  ArrowRightIcon,
  CheckCircle2Icon,
  DatabaseIcon,
  Loader2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { LegacyMigrationStats } from "../../../preload/api/migration";
import { Modal } from "../ui/Modal";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useTagStore } from "../../stores/tag.store";

interface MigrationDialogProps {
  isOpen: boolean;
  itemCount: number;
  onClose: () => void;
}

type Phase = "confirm" | "running" | "done" | "error";

/**
 * 旧版归知数据一次性迁移对话框（首次启动检测到旧库且新库为空时弹出）。
 */
export function MigrationDialog({
  isOpen,
  itemCount,
  onClose,
}: MigrationDialogProps) {
  const { t } = useTranslation();
  const [phase, setPhase] = useState<Phase>("confirm");
  const [stats, setStats] = useState<LegacyMigrationStats | null>(null);
  const [error, setError] = useState("");

  const runMigration = async () => {
    setPhase("running");
    try {
      const result = await window.api.migration.runLegacy();
      setStats(result);
      setPhase("done");
      // 迁移完成后刷新各视图数据
      await Promise.all([
        useKnowledgeStore.getState().refreshAll(),
        useCollectionStore.getState().fetchCollections(),
        useTagStore.getState().fetchTags(),
      ]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setPhase("error");
    }
  };

  const dismiss = () => {
    // 「暂不迁移」记住选择，之后不再自动弹出
    if (phase === "confirm") {
      localStorage.setItem("guizhi-migration-dismissed", "1");
    }
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={phase === "running" ? () => {} : dismiss}
      title={t("migration.title", "发现旧版归知数据")}
      size="md"
    >
      {phase === "confirm" ? (
        <div className="space-y-4">
          <div className="flex items-start gap-3 rounded-xl border border-border/70 bg-background/60 px-4 py-3">
            <DatabaseIcon
              className="mt-0.5 h-5 w-5 shrink-0 text-primary"
              aria-hidden="true"
            />
            <div className="space-y-1 text-sm text-foreground">
              <p>
                {t("migration.detected", "检测到旧版归知的数据库（{{count}} 条知识条目）。", {
                  count: itemCount,
                })}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(
                  "migration.scope",
                  "可一键迁入：知识条目、知识库、标签、来源记录与 Wiki 页面。原数据文件保持不变。",
                )}
              </p>
            </div>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={dismiss}
              className="inline-flex h-9 items-center rounded-lg border border-border px-4 text-sm text-foreground transition-colors hover:bg-muted/60"
            >
              {t("migration.skip", "暂不迁移")}
            </button>
            <button
              type="button"
              onClick={() => void runMigration()}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              <ArrowRightIcon className="h-4 w-4" aria-hidden="true" />
              {t("migration.start", "立即迁移")}
            </button>
          </div>
        </div>
      ) : null}

      {phase === "running" ? (
        <div className="flex flex-col items-center gap-3 py-6">
          <Loader2Icon
            className="h-8 w-8 animate-spin text-primary"
            aria-hidden="true"
          />
          <p className="text-sm text-muted-foreground">
            {t("migration.running", "正在迁移数据…")}
          </p>
        </div>
      ) : null}

      {phase === "done" && stats ? (
        <div className="space-y-4">
          <div className="flex items-start gap-3 rounded-xl border border-green-500/20 bg-green-500/5 px-4 py-3">
            <CheckCircle2Icon
              className="mt-0.5 h-5 w-5 shrink-0 text-green-600 dark:text-green-400"
              aria-hidden="true"
            />
            <div className="space-y-1 text-sm">
              <p className="text-foreground">
                {t("migration.done", "迁移完成")}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(
                  "migration.doneStats",
                  "条目 {{items}} · 知识库 {{collections}} · 标签 {{tags}} · 来源 {{sources}} · Wiki 页面 {{wikiPages}}",
                  {
                    items: stats.items,
                    collections: stats.collections,
                    tags: stats.tags,
                    sources: stats.sources,
                    wikiPages: stats.wikiPages,
                  },
                )}
              </p>
            </div>
          </div>
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              {t("migration.finish", "开始使用")}
            </button>
          </div>
        </div>
      ) : null}

      {phase === "error" ? (
        <div className="space-y-4">
          <div className="flex items-start gap-3 rounded-xl border border-destructive/20 bg-destructive/5 px-4 py-3">
            <TriangleAlertIcon
              className="mt-0.5 h-5 w-5 shrink-0 text-destructive"
              aria-hidden="true"
            />
            <div className="space-y-1 text-sm">
              <p className="text-foreground">
                {t("migration.failed", "迁移失败")}
              </p>
              <p className="break-all text-xs text-destructive/90">{error}</p>
            </div>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={dismiss}
              className="inline-flex h-9 items-center rounded-lg border border-border px-4 text-sm text-foreground transition-colors hover:bg-muted/60"
            >
              {t("common.close", "关闭")}
            </button>
            <button
              type="button"
              onClick={() => void runMigration()}
              className="inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              {t("imports.retry", "重试")}
            </button>
          </div>
        </div>
      ) : null}
    </Modal>
  );
}
