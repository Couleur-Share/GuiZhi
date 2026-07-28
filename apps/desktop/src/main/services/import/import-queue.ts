/**
 * 导入队列：并发调度 + DB 持久化 + 重启恢复 + 取消/重试 + 变更广播。
 *
 * 依赖以接口注入，便于在单测中用假实现驱动调度逻辑。
 */
import { randomUUID } from "crypto";
import type {
  EnqueueImportInput,
  ImportStage,
  ImportTask,
} from "@guizhi/shared/types";
import type { ExtractedContent } from "./connectors";
import { normalizeUrl } from "./url-normalize";
import { computeContentHash } from "./content-hash";

export interface ImportTaskStore {
  create(input: EnqueueImportInput): ImportTask;
  get(id: string): ImportTask | null;
  isForceDuplicate(id: string): boolean;
  list(limit?: number): ImportTask[];
  listByStatus(statuses: ImportTask["status"][]): ImportTask[];
  update(
    id: string,
    patch: Partial<{
      status: ImportTask["status"];
      stage: ImportStage | null;
      error: string | null;
      warning: string | null;
      displayName: string;
      itemType: ImportTask["itemType"];
      resultItemId: string | null;
      duplicateItemId: string | null;
      forceDuplicate: boolean;
    }>,
  ): ImportTask | null;
  resetProcessingToPending(): number;
}

export interface ImportPersistence {
  /** 按规范化 URI 或内容哈希查找既有条目 id（去重判定） */
  findDuplicate(
    normalizedUri: string | null,
    contentHash: string,
  ): string | null;
  /** 入库并写来源记录，返回条目 id */
  saveItem(params: {
    extracted: ExtractedContent;
    collectionId: string | null;
    tagNames: string[];
    sourceKind: ImportTask["sourceKind"];
    normalizedUri: string | null;
    contentHash: string;
  }): string;
}

export interface ImportQueueOptions {
  store: ImportTaskStore;
  persistence: ImportPersistence;
  extract: (
    kind: ImportTask["sourceKind"],
    input: string,
    signal: AbortSignal,
    onStage: (stage: ImportStage) => void,
  ) => Promise<ExtractedContent>;
  onTaskChanged: (task: ImportTask) => void;
  concurrency?: number;
}

const DEFAULT_CONCURRENCY = 2;

export class ImportQueue {
  private readonly store: ImportTaskStore;
  private readonly persistence: ImportPersistence;
  private readonly extract: ImportQueueOptions["extract"];
  private readonly onTaskChanged: (task: ImportTask) => void;
  private readonly concurrency: number;

  private readonly pendingIds: string[] = [];
  private readonly running = new Map<string, AbortController>();

  constructor(options: ImportQueueOptions) {
    this.store = options.store;
    this.persistence = options.persistence;
    this.extract = options.extract;
    this.onTaskChanged = options.onTaskChanged;
    this.concurrency = options.concurrency ?? DEFAULT_CONCURRENCY;
  }

  /**
   * 启动恢复：把上次遗留的 processing 复位并连同 pending 一起入队。
   *
   * 必须按状态查询——list() 是「最近 200 条」，一次入队几百个文件后重启，
   * 最早的那批 pending 会落在 200 条窗口之外，永远不再被调度。
   */
  recover(): void {
    this.store.resetProcessingToPending();
    const pending = this.store
      .listByStatus(["pending"])
      .sort((left, right) => left.createdAt - right.createdAt);
    for (const task of pending) {
      if (!this.pendingIds.includes(task.id)) {
        this.pendingIds.push(task.id);
      }
    }
    this.pump();
  }

  enqueue(inputs: EnqueueImportInput[]): ImportTask[] {
    const tasks: ImportTask[] = [];
    for (const input of inputs) {
      const value = input.input?.trim();
      if (!value) {
        continue;
      }
      const task = this.store.create({ ...input, input: value });
      tasks.push(task);
      this.pendingIds.push(task.id);
      this.onTaskChanged(task);
    }
    this.pump();
    return tasks;
  }

  cancel(id: string): boolean {
    const controller = this.running.get(id);
    if (controller) {
      controller.abort();
      return true;
    }
    const index = this.pendingIds.indexOf(id);
    if (index >= 0) {
      this.pendingIds.splice(index, 1);
      const task = this.store.update(id, { status: "canceled", stage: null });
      if (task) {
        this.onTaskChanged(task);
      }
      return true;
    }
    return false;
  }

  /** 重试失败/取消的任务；`forceDuplicate` 用于「仍要创建副本」。 */
  retry(id: string, options?: { forceDuplicate?: boolean }): ImportTask | null {
    const existing = this.store.get(id);
    if (!existing) {
      return null;
    }
    if (existing.status === "pending" || existing.status === "processing") {
      return existing;
    }
    const task = this.store.update(id, {
      status: "pending",
      stage: null,
      error: null,
      warning: null,
      duplicateItemId: null,
      ...(options?.forceDuplicate !== undefined
        ? { forceDuplicate: options.forceDuplicate }
        : {}),
    });
    if (task) {
      this.pendingIds.push(task.id);
      this.onTaskChanged(task);
      this.pump();
    }
    return task;
  }

  /** 等待所有在途任务结束（测试与关停用） */
  async drain(): Promise<void> {
    while (this.running.size > 0 || this.pendingIds.length > 0) {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }

  private pump(): void {
    while (this.running.size < this.concurrency && this.pendingIds.length > 0) {
      const id = this.pendingIds.shift()!;
      const controller = new AbortController();
      this.running.set(id, controller);
      void this.runTask(id, controller).finally(() => {
        this.running.delete(id);
        this.pump();
      });
    }
  }

  private updateAndNotify(
    id: string,
    patch: Parameters<ImportTaskStore["update"]>[1],
  ): void {
    const task = this.store.update(id, patch);
    if (task) {
      this.onTaskChanged(task);
    }
  }

  private async runTask(
    id: string,
    controller: AbortController,
  ): Promise<void> {
    const task = this.store.get(id);
    if (!task || task.status !== "pending") {
      return;
    }

    this.updateAndNotify(id, { status: "processing", stage: "fetching" });

    try {
      this.throwIfAborted(controller);
      const extracted = await this.extract(
        task.sourceKind,
        task.sourceInput,
        controller.signal,
        (stage) => this.updateAndNotify(id, { stage }),
      );

      this.throwIfAborted(controller);
      // 标题与类型在这里回写：建任务时只有原始 URL，一列长得一样的链接
      // 没法辨认采的是什么。写在去重判定之前，重复任务同样拿得到标题。
      this.updateAndNotify(id, {
        stage: "extracting",
        displayName: extracted.title,
        itemType: extracted.itemType,
      });

      // 降级结果按失败处理：不入库，也不登记来源，
      // 否则空壳条目会占住该链接的 normalized_uri，重试永远判重为重复
      if (extracted.degradedReason) {
        this.updateAndNotify(id, {
          status: "failed",
          stage: null,
          error: extracted.degradedReason,
        });
        return;
      }

      const normalizedUri =
        task.sourceKind === "url" && extracted.sourceUri
          ? normalizeUrl(extracted.sourceUri)
          : null;
      const contentHash = computeContentHash(
        extracted.content || extracted.title,
      );

      if (!this.store.isForceDuplicate(id)) {
        const duplicateItemId = this.persistence.findDuplicate(
          normalizedUri,
          contentHash,
        );
        if (duplicateItemId) {
          this.updateAndNotify(id, {
            status: "duplicate",
            stage: null,
            duplicateItemId,
          });
          return;
        }
      }

      this.throwIfAborted(controller);
      this.updateAndNotify(id, { stage: "saving" });

      const resultItemId = this.persistence.saveItem({
        extracted,
        collectionId: task.collectionId ?? null,
        tagNames: task.tagNames ?? [],
        sourceKind: task.sourceKind,
        normalizedUri,
        contentHash,
      });

      this.updateAndNotify(id, {
        status: "completed",
        stage: null,
        // 入库了但内容有缺失时，「已完成」这三个字必须带上下文
        warning: extracted.warningReason ?? null,
        resultItemId,
      });
    } catch (error) {
      if (controller.signal.aborted) {
        this.updateAndNotify(id, { status: "canceled", stage: null });
        return;
      }
      const message = error instanceof Error ? error.message : String(error);
      this.updateAndNotify(id, {
        status: "failed",
        stage: null,
        error: message,
      });
    }
  }

  private throwIfAborted(controller: AbortController): void {
    if (controller.signal.aborted) {
      throw new Error("已取消");
    }
  }
}

/** 生成 source_records 主键（供持久化实现使用） */
export function createSourceRecordId(): string {
  return randomUUID();
}
