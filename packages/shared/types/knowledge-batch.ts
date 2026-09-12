import type { BulkUpdateKnowledgeItemsInput, KnowledgeItemQuery, KnowledgeItemStatus } from './knowledge';
export type KnowledgeBatchAction = { kind: 'update'; patch: BulkUpdateKnowledgeItemsInput } | { kind: 'status'; status: KnowledgeItemStatus } | { kind: 'trash' | 'restore' } | { kind: 'delete'; clearEvidence?: boolean };
export interface KnowledgeBatchResult { id: string; ok: boolean; error?: string; }
export type KnowledgeSelectionCommand = { action: 'results'; runId: string } | { action: 'source-versions'; id: string } | { action: 'freeze'; query: KnowledgeItemQuery } | { action: 'execute'; runId?: string; ids: string[]; command: KnowledgeBatchAction };
