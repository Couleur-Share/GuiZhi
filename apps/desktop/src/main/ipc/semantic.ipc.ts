/**
 * 语义索引 IPC：状态 / 待索引批次 / 向量落库 / 余弦检索。
 */
import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { ApplySemanticEmbeddingsInput } from "@guizhi/shared/types";
import { SemanticIndexDB } from "@guizhi/db";
import Database from "../database/sqlite";
import {
  getSemanticStatus,
  listPendingSemanticItems,
  searchSemanticByVector,
} from "../services/semantic";

const PENDING_BATCH_MAX = 50;
const SEARCH_LIMIT_MAX = 20;

function isValidApplyInput(
  input: unknown,
): input is ApplySemanticEmbeddingsInput {
  if (!input || typeof input !== "object") {
    return false;
  }
  const candidate = input as ApplySemanticEmbeddingsInput;
  return (
    typeof candidate.itemId === "string" &&
    typeof candidate.contentHash === "string" &&
    typeof candidate.model === "string" &&
    Number.isInteger(candidate.dims) &&
    candidate.dims > 0 &&
    Array.isArray(candidate.chunks) &&
    candidate.chunks.length > 0 &&
    candidate.chunks.every(
      (chunk) =>
        typeof chunk.text === "string" &&
        Array.isArray(chunk.vector) &&
        chunk.vector.length === candidate.dims,
    )
  );
}

export function registerSemanticIPC(db: Database.Database): void {
  const semantic = new SemanticIndexDB(db);

  ipcMain.handle(IPC_CHANNELS.SEMANTIC_STATUS, (_event, model: string) =>
    getSemanticStatus(db, typeof model === "string" ? model : ""),
  );

  ipcMain.handle(
    IPC_CHANNELS.SEMANTIC_LIST_PENDING,
    (_event, params: { model?: unknown; limit?: unknown }) => {
      const model = typeof params?.model === "string" ? params.model : "";
      const limit = Math.min(
        Math.max(1, Number(params?.limit) || 10),
        PENDING_BATCH_MAX,
      );
      return listPendingSemanticItems(db, model, limit);
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.SEMANTIC_APPLY_EMBEDDINGS,
    (_event, input: ApplySemanticEmbeddingsInput): boolean => {
      if (!isValidApplyInput(input)) {
        throw new Error("semantic:applyEmbeddings 载荷不合法");
      }
      try {
        semantic.replaceItemChunks({
          itemId: input.itemId,
          contentHash: input.contentHash,
          model: input.model,
          dims: input.dims,
          chunks: input.chunks.map((chunk) => ({
            text: chunk.text,
            vector: new Float32Array(chunk.vector),
          })),
        });
        return true;
      } catch (error) {
        // 条目可能在嵌入期间被彻底删除（外键约束失败），跳过即可
        console.warn(
          `[semantic] 向量落库失败（item=${input.itemId}）:`,
          error instanceof Error ? error.message : error,
        );
        return false;
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.SEMANTIC_SEARCH,
    (
      _event,
      params: { model?: unknown; vector?: unknown; limit?: unknown },
    ) => {
      const model = typeof params?.model === "string" ? params.model : "";
      const vector = Array.isArray(params?.vector)
        ? (params.vector as number[])
        : [];
      if (!model || vector.length === 0) {
        return [];
      }
      const limit = Math.min(
        Math.max(1, Number(params?.limit) || 5),
        SEARCH_LIMIT_MAX,
      );
      return searchSemanticByVector(db, model, new Float32Array(vector), limit);
    },
  );

  ipcMain.handle(IPC_CHANNELS.SEMANTIC_CLEAR, () => semantic.clearAll());
}
