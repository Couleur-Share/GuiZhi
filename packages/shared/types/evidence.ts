/** 回答时实际提供给模型的片段。缺少此结构的老消息不得使用现正文冒充证据。 */
export interface EvidenceSnapshot {
  version: 1;
  kind: 'item' | 'wiki' | 'article' | 'web';
  sourceId?: string;
  sourceItemIds?: string[];
  title: string;
  text: string;
  fingerprint: string;
  sourceVersion?: string;
  capturedAt: number;
  reviewStatus: 'clear' | 'needs_review';
  reviewReasons: string[];
  url?: string;
  cleared?: boolean;
}
