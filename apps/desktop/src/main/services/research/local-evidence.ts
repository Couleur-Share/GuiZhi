import { KnowledgeItemDB, ResearchDB, ResearchWorkflowDB, SemanticIndexDB } from "@guizhi/db";
import type Database from "../../database/sqlite";
import type { ResearchRun, ResearchLocalEvidence } from "@guizhi/shared/types";
import { mergeHybridResults, type QaSearchHit } from "@guizhi/shared/utils/hybrid-results";
import { l2Normalize, parseEmbeddingsResponse } from "@guizhi/shared/utils/embedding-values";
import { buildEmbeddingsEndpointFromBase, buildHeadersForProtocol, resolveAIProtocol, resolveProtocolBase } from "@guizhi/shared/utils/ai-protocol";
import { ExactSemanticSearchBackend } from "../semantic-search-backend";
import { fetchWithNetworkProxy } from "../network-proxy";
import { recordMainAiUsage } from "../ai-usage";
import { researchModel } from "./research-ai";
import { withinResearchBudget } from "./budget";
import { selectPassages } from "./report-evidence";

export async function embedResearchQuery(topic: string, signal: AbortSignal): Promise<{ model: string; vector: number[] } | null> {
  const config = researchModel(true);
  if (!config) return null;
  const protocol = resolveAIProtocol(config);
  const url = buildEmbeddingsEndpointFromBase(resolveProtocolBase(config.apiUrl, protocol));
  if (!url) return null;
  let body: string;
  try {
    body = await withinResearchBudget(signal, 30_000, async (child) => {
      const response = await fetchWithNetworkProxy(url, { method: "POST", headers: buildHeadersForProtocol(protocol, config.apiKey), body: JSON.stringify({ model: config.model, input: [topic] }), signal: child });
      if (!response.ok) throw new Error(`Embedding HTTP ${response.status}`);
      return response.text();
    });
  } catch (error) { recordMainAiUsage({ scenario: "embedding", model: config.model, failed: true }); throw error; }
  const value = parseEmbeddingsResponse(body, 1)[0];
  if (!value.every((n) => typeof n === "number" && Number.isFinite(n))) throw new Error("Embedding 向量不合法");
  recordMainAiUsage({ scenario: "embedding", model: config.model, promptTokens: Number(JSON.parse(body).usage?.prompt_tokens) || 0 });
  return { model: config.model, vector: l2Normalize(value) };
}

/** Reads the existing index, scoped in SQL before ranking; never writes an index. */
export function createLocalResearchEvidence(db: Database.Database, embed = embedResearchQuery) {
  return async (run: ResearchRun, signal: AbortSignal): Promise<ResearchLocalEvidence[]> => {
    const scope = run.context?.knowledgeScope;
    if (!scope) return [];
    const items = new KnowledgeItemDB(db);
    const workflow = new ResearchWorkflowDB(db);
    const excludedIds = [...new Set([...workflow.savedReportIds(run.context?.seriesId ?? run.id), ...workflow.series(run.id).map((id) => new ResearchDB(db).get(id)?.savedItemId).filter((id): id is string => Boolean(id))])];
    const allowed = (id: string) => {
      const item = items.get(id);
      return item && !item.deletedAt && !excludedIds.includes(id) && (scope.kind === "all" || item.collectionId === scope.collectionId) ? item : null;
    };
    const fts = items.list({ scope: "all", search: run.topic, searchMode: "recall", includeArchived: true, collectionScope: scope.kind === "collection" ? { ids: [scope.collectionId], includeUncategorized: false } : undefined, excludedItemIds: excludedIds, limit: 24 }).entries;
    const ftsHits = fts.map((i) => ({ id: i.id, title: i.title, snippet: i.snippet }));
    const semanticHits: QaSearchHit[] = [];
    try {
      const query = await embed(run.topic, signal);
      signal.throwIfAborted();
      if (query) {
        const index = new SemanticIndexDB(db);
        let cursor = 0;
        const best = new Map<string, { itemId: string; chunkIndex: number; score: number }>();
        for (;;) {
          signal.throwIfAborted();
          const page = index.loadVectorsForSearch(query.model, 500, cursor, { collectionId: scope.kind === "collection" ? scope.collectionId : undefined, excludedIds });
          if (!page.length) break;
          cursor = page[page.length - 1].rowid;
          const rows = page.filter((r) => r.vector.length === query.vector.length);
          const vectors = new Float32Array(rows.length * query.vector.length);
          rows.forEach((r, i) => vectors.set(r.vector, i * query.vector.length));
          const backend = new ExactSemanticSearchBackend({ model: query.model, dims: query.vector.length, vectors, itemIds: rows.map((r) => r.itemId), chunkIndexes: rows.map((r) => r.chunkIndex) });
          for (const hit of await backend.search(new Float32Array(query.vector), 24)) if (hit.score >= .25 && hit.score > (best.get(hit.itemId)?.score ?? -1)) best.set(hit.itemId, hit);
          await new Promise<void>((resolve) => setImmediate(resolve));
        }
        const top = [...best.values()].sort((a, b) => b.score - a.score).slice(0, 24);
        const snippets = index.loadChunkSnippets(top);
        for (const hit of top) {
          const text = snippets.find((s) => s.itemId === hit.itemId && s.chunkIndex === hit.chunkIndex);
          if (text) semanticHits.push({ id: hit.itemId, title: text.title, snippet: text.chunkText, matchText: text.chunkText });
        }
      }
    } catch { signal.throwIfAborted(); /* Semantic is optional; keep scoped FTS results. */ }
    signal.throwIfAborted();
    const output: ResearchLocalEvidence[] = [];
    for (const hit of mergeHybridResults(ftsHits, semanticHits, 48)) {
      const item = allowed(hit.id); // Re-check after the asynchronous query.
      if (!item) continue;
      const text = hit.matchText && item.content.includes(hit.matchText) ? hit.matchText : item.content;
      const excerpt = selectPassages(text.split(/\n\s*\n/).map((text, position) => ({ text, position, kind: "local" })), run.topic, 1000).map((p) => p.text).join("\n").slice(0, 1000);
      output.push({ ref: `L${output.length + 1}`, itemId: item.id, title: item.title, excerpt, updatedAt: item.updatedAt, capturedAt: item.createdAt, url: item.sourceUri ?? undefined });
      if (output.length === 6) break;
    }
    return output;
  };
}
