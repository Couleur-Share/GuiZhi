/**
 * 正文配图 IPC：风格预设、配图策划、逐张生成、单张重生成与移除。
 *
 * 生成放在主进程而不是渲染进程：整条链路是「读库中正文 → 调模型 → 写资产文件
 * → 写回条目」，后两步本来就只有主进程做得了，形态与 media:summarize 一致。
 */
import { ipcMain, shell, type IpcMainInvokeEvent } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import {
  BUILT_IN_ILLUSTRATION_STYLES,
  coreIllustrationStyleService,
} from "@guizhi/core";
import {
  findIllustrationAnchor,
  listAnchorBlocks,
  listIllustrations,
  removeIllustration,
} from "@guizhi/shared/utils/illustration-note";
import { parseIllustrationShots } from "@guizhi/shared/utils/illustration-prompt";
import type {
  AIProtocol,
  IllustrationGenerateResult,
  IllustrationPlanResult,
  IllustrationShot,
  IllustrationStyle,
  KnowledgeItem,
} from "@guizhi/shared/types";
import { KnowledgeItemDB } from "@guizhi/db";
import { logAppError } from "../diagnostic-log";
import type Database from "../database/sqlite";
import { cleanupOrphanAssets } from "../services/asset-cleanup";
import { resolveMediaSummaryConfig } from "../services/media/media-summary";
import {
  resolveImageGenConfig,
  testImageGenConfig,
} from "../services/illustration/image-gen";
import {
  planIllustrations,
  type IllustrationShotCount,
} from "../services/illustration/plan";
import {
  regenerateIllustration,
  runIllustrations,
} from "../services/illustration/runner";

/** 在途生成：条目 id → controller，供渲染进程按条目中断 */
const inflight = new Map<string, AbortController>();

function beginRun(itemId: string): AbortController {
  inflight.get(itemId)?.abort(new Error("已取消"));
  const controller = new AbortController();
  inflight.set(itemId, controller);
  return controller;
}

function endRun(itemId: string, controller: AbortController): void {
  if (inflight.get(itemId) === controller) {
    inflight.delete(itemId);
  }
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * 配图链路的失败留痕。
 *
 * 这几处都会弹 toast，但 toast 关掉就什么都不剩，事后既数不出失败率、
 * 也回溯不了当时的报错。逐张生成失败记在 runner 里，这里管的是整条链路
 * 抛出来的异常。
 */
function logIllustrationFailure(
  action: string,
  itemId: string,
  error: unknown,
): void {
  logAppError({
    scope: "illustration",
    action,
    message: describe(error),
    itemId,
  });
}

function readItemId(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** 面板传上来的张数；非正整数一律当「自动」 */
function readShotCount(value: unknown): IllustrationShotCount {
  const count = Math.trunc(Number(value));
  return Number.isFinite(count) && count > 0 ? count : "auto";
}

interface ItemContext {
  items: KnowledgeItemDB;
  item: KnowledgeItem;
  style: IllustrationStyle;
}

/** 取条目与风格预设；任一缺失时返回可直接回传的错误结果 */
function resolveContext(
  db: Database.Database,
  itemId: string,
  styleId: unknown,
): ItemContext | { error: string } {
  const items = new KnowledgeItemDB(db);
  const item = itemId ? items.get(itemId) : null;
  if (!item) {
    return { error: "条目不存在" };
  }
  const style = coreIllustrationStyleService.find(
    typeof styleId === "string" ? styleId : undefined,
  );
  if (!style) {
    return { error: "没有可用的配图风格预设" };
  }
  return { items, item, style };
}

/**
 * 校验渲染进程回传的 shot list。
 *
 * 面板允许用户改图题与标注词，回来的东西不能直接信：复用策划输出那套清洗，
 * 序号仍贴回候选集合、限长限张数完全一致。
 */
function validateShots(
  raw: unknown,
  allowedBlocks: number[],
  style: IllustrationStyle,
): IllustrationShot[] {
  if (!Array.isArray(raw) || raw.length === 0) {
    return [];
  }
  return parseIllustrationShots(JSON.stringify(raw), {
    allowedBlocks,
    maxShots: style.maxShots,
    maxLabels: style.maxLabels,
  });
}

export interface IllustrationTestResult {
  success: boolean;
  latency?: number;
  error?: string;
}

export function registerIllustrationIPC(db: Database.Database): void {
  ipcMain.handle(IPC_CHANNELS.ILLUSTRATION_STYLES, () =>
    coreIllustrationStyleService.read(),
  );

  // 文生图模型不能用 chat completions 测——provider 会直接回 model_not_supported
  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_TEST,
    async (
      _event,
      config: {
        apiUrl?: string;
        apiKey?: string;
        model?: string;
        apiProtocol?: AIProtocol;
        provider?: string;
      },
    ): Promise<IllustrationTestResult> => {
      const apiUrl = config?.apiUrl?.trim();
      const apiKey = config?.apiKey?.trim();
      const model = config?.model?.trim();
      if (!apiUrl || !apiKey || !model) {
        return { success: false, error: "文生图模型配置不完整" };
      }
      try {
        const { latency } = await testImageGenConfig({
          apiUrl,
          apiKey,
          model,
          apiProtocol: config.apiProtocol,
          provider: config.provider,
        });
        return { success: true, latency };
      } catch (error) {
        return { success: false, error: describe(error) };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_SAVE_STYLES,
    (_event, payload: { styles?: unknown }) =>
      coreIllustrationStyleService.write(payload?.styles),
  );

  // 只回内置预设不落盘：编辑器里「恢复内置预设」之后仍能按「取消」反悔
  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_BUILT_IN_STYLES,
    () => BUILT_IN_ILLUSTRATION_STYLES,
  );

  // 定位而不是打开：.json 在 Windows 上多半没有默认关联程序，
  // shell.openPath 会弹出系统的「选择一个应用」框
  ipcMain.handle(IPC_CHANNELS.ILLUSTRATION_REVEAL_STYLES_FILE, () => {
    try {
      shell.showItemInFolder(coreIllustrationStyleService.ensureFile());
      return { success: true };
    } catch (error) {
      return { success: false, error: describe(error) };
    }
  });

  // registerAllIPC 会在切换数据目录时重入，`on` 不像 `handle` 会自动覆盖，
  // 不先清一遍就会越积越多个监听器
  ipcMain.removeAllListeners(IPC_CHANNELS.ILLUSTRATION_CANCEL);
  ipcMain.on(IPC_CHANNELS.ILLUSTRATION_CANCEL, (_event, itemId: unknown) => {
    const id = readItemId(itemId);
    if (id) {
      inflight.get(id)?.abort(new Error("已取消"));
    }
  });

  // 只出配图规格，不生成图片：这一步便宜又快，让用户先砍掉不要的
  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_PLAN,
    async (
      _event,
      payload: { itemId?: unknown; styleId?: unknown; shotCount?: unknown },
    ): Promise<IllustrationPlanResult> => {
      const itemId = readItemId(payload?.itemId);
      const context = resolveContext(db, itemId, payload?.styleId);
      if ("error" in context) {
        return { success: false, error: context.error };
      }

      const config = resolveMediaSummaryConfig();
      if (!config) {
        return {
          success: false,
          notConfigured: true,
          error: "未配置可用的文本模型",
        };
      }

      const controller = beginRun(itemId);
      try {
        // 模型已经在读正文了，顺带让它看一眼风格清单给个建议，零额外调用
        const plan = await planIllustrations(
          {
            title: context.item.title,
            blocks: listAnchorBlocks(context.item.content),
            style: context.style,
            shotCount: readShotCount(payload?.shotCount),
            catalog: coreIllustrationStyleService.read(),
          },
          config,
          { signal: controller.signal },
        );
        return {
          success: true,
          shots: plan.shots,
          styleId: context.style.id,
          suggestedStyleId: plan.suggestedStyleId || undefined,
        };
      } catch (error) {
        logIllustrationFailure("配图策划", itemId, error);
        return { success: false, error: describe(error) };
      } finally {
        endRun(itemId, controller);
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_GENERATE,
    async (
      event: IpcMainInvokeEvent,
      payload: { itemId?: unknown; styleId?: unknown; shots?: unknown },
    ): Promise<IllustrationGenerateResult> => {
      const itemId = readItemId(payload?.itemId);
      const context = resolveContext(db, itemId, payload?.styleId);
      if ("error" in context) {
        return { success: false, error: context.error };
      }

      const allowedBlocks = listAnchorBlocks(context.item.content).map(
        (block) => block.index,
      );
      const shots = validateShots(payload?.shots, allowedBlocks, context.style);
      if (shots.length === 0) {
        return { success: false, error: "没有可生成的配图规格" };
      }

      const config = resolveImageGenConfig();
      if (!config) {
        return {
          success: false,
          notConfigured: true,
          error: "未配置文生图模型",
        };
      }

      const controller = beginRun(itemId);
      try {
        const result = await runIllustrations(
          context.items,
          context.item,
          shots,
          context.style,
          config,
          {
            signal: controller.signal,
            onProgress: (progress) => {
              if (!event.sender.isDestroyed()) {
                event.sender.send(IPC_CHANNELS.ILLUSTRATION_PROGRESS, {
                  ...progress,
                  itemId,
                });
              }
            },
          },
        );
        if (result.generated > 0) {
          return {
            success: true,
            item: result.item ?? undefined,
            generated: result.generated,
            failures: result.failures,
          };
        }
        return {
          success: false,
          failures: result.failures,
          error: result.aborted ? "已取消" : "所有配图都生成失败",
        };
      } catch (error) {
        logIllustrationFailure("正文配图", itemId, error);
        return { success: false, error: describe(error) };
      } finally {
        endRun(itemId, controller);
      }
    },
  );

  // 重新生成某一张：对它所依附的那一段重新策划，再原位换图
  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_REGENERATE,
    async (
      _event,
      payload: {
        itemId?: unknown;
        styleId?: unknown;
        assetFileName?: unknown;
      },
    ): Promise<IllustrationGenerateResult> => {
      const itemId = readItemId(payload?.itemId);
      const assetFileName = readItemId(payload?.assetFileName);
      const context = resolveContext(db, itemId, payload?.styleId);
      if ("error" in context) {
        return { success: false, error: context.error };
      }

      const anchor = findIllustrationAnchor(context.item.content, assetFileName);
      if (!anchor) {
        return { success: false, error: "找不到这张配图所在的段落" };
      }

      const planConfig = resolveMediaSummaryConfig();
      const imageConfig = resolveImageGenConfig();
      if (!planConfig || !imageConfig) {
        return {
          success: false,
          notConfigured: true,
          error: planConfig ? "未配置文生图模型" : "未配置可用的文本模型",
        };
      }

      const controller = beginRun(itemId);
      try {
        const { shots } = await planIllustrations(
          {
            title: context.item.title,
            blocks: [anchor],
            style: context.style,
            shotCount: 1,
          },
          planConfig,
          { signal: controller.signal },
        );
        const [shot] = shots;
        const updated = await regenerateIllustration(
          context.items,
          context.item,
          assetFileName,
          shot,
          context.style,
          imageConfig,
          controller.signal,
        );
        // 换下来的旧图不再被引用，随手回收，别在磁盘上越攒越多
        cleanupOrphanAssets(context.items, [assetFileName]);
        return { success: true, item: updated ?? undefined, generated: 1 };
      } catch (error) {
        logIllustrationFailure("重新生成配图", itemId, error);
        return { success: false, error: describe(error) };
      } finally {
        endRun(itemId, controller);
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_REMOVE,
    (
      _event,
      payload: { itemId?: unknown; assetFileName?: unknown },
    ): IllustrationGenerateResult => {
      const itemId = readItemId(payload?.itemId);
      const assetFileName = readItemId(payload?.assetFileName);
      const items = new KnowledgeItemDB(db);
      const item = itemId ? items.get(itemId) : null;
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      const content = removeIllustration(item.content, assetFileName);
      if (content === item.content) {
        return { success: false, error: "正文里没有这张配图" };
      }
      const updated = items.update(item.id, { content });
      cleanupOrphanAssets(items, [assetFileName]);
      return { success: true, item: updated ?? undefined };
    },
  );

  // 一次清空：逐张删要走 N 趟 IPC、写 N 次正文，中途失败还会留下删一半的结果
  ipcMain.handle(
    IPC_CHANNELS.ILLUSTRATION_CLEAR,
    (_event, payload: { itemId?: unknown }): IllustrationGenerateResult => {
      const itemId = readItemId(payload?.itemId);
      const items = new KnowledgeItemDB(db);
      const item = itemId ? items.get(itemId) : null;
      if (!item) {
        return { success: false, error: "条目不存在" };
      }
      const entries = listIllustrations(item.content);
      if (entries.length === 0) {
        return { success: false, error: "正文里没有配图" };
      }
      const content = entries.reduce(
        (text, entry) => removeIllustration(text, entry.assetFileName),
        item.content,
      );
      const updated = items.update(item.id, { content });
      cleanupOrphanAssets(
        items,
        entries.map((entry) => entry.assetFileName),
      );
      return {
        success: true,
        item: updated ?? undefined,
        removed: entries.length,
      };
    },
  );
}
