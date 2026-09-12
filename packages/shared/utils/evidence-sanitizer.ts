import { evidenceText, evidenceUrl } from './evidence';

export function sanitizeEvidenceTarget<T extends { selection?: string } | undefined>(target: T): T {
  return target ? { ...target, ...(target.selection ? { selection: evidenceText(target.selection) } : {}) } : target;
}

/** 只处理结构化证据，回答文本原样保留。清除标记阻止在途旧保存重新写入片段。 */
export function sanitizeEvidenceJson(json: string, cleared: ReadonlySet<string>): string {
  const messages: unknown = JSON.parse(json);
  if (!Array.isArray(messages)) throw new Error('会话消息格式无效');
  const source = (raw: any, inheritedId?: string): any => {
    if (!raw || typeof raw !== 'object') return raw;
    const id = raw.sourceId ?? raw.target?.itemId ?? (raw.kind === 'item' ? raw.refId : undefined) ?? inheritedId;
    if ((id && cleared.has(id)) || (raw.sourceItemIds ?? raw.evidence?.sourceItemIds ?? []).some((itemId: string) => cleared.has(itemId))) {
      return { ordinal: raw.ordinal, kind: raw.kind, refId: raw.refId, sourceId: id, title: '来源已清除',
        cleared: true, ...(raw.evidence ? { evidence: { version: 1, kind: raw.kind, sourceId: id, title: '来源已清除', text: '', fingerprint: '', capturedAt: raw.evidence.capturedAt, reviewStatus: 'clear', reviewReasons: [], cleared: true } } : {}) };
    }
    return { ...raw, ...(typeof raw.text === 'string' ? { text: evidenceText(raw.text) } : {}),
      ...(raw.url ? { url: evidenceUrl(raw.url) } : {}), ...(raw.target ? { target: sanitizeEvidenceTarget(raw.target) } : {}), ...(raw.evidence ? { evidence: source(raw.evidence, id) } : {}) };
  };
  return JSON.stringify(messages.map((message: any) => {
    if (!message || typeof message !== 'object') return message;
    const context = message.context;
    const contextCleared = context?.target?.itemId && cleared.has(context.target.itemId);
    return { ...message, ...(Array.isArray(message.evidenceSources) ? { evidenceSources: message.evidenceSources.map((s: any) => source(s)) } : {}), ...(Array.isArray(message.sources) ? { sources: message.sources.map((s: any) => source(s)) } : {}),
      ...(context ? { evidenceCleared: contextCleared, context: contextCleared ? undefined : { ...context, target: sanitizeEvidenceTarget(context.target), sources: context.sources?.map((s: any) => source(s, context.target?.itemId)) } } : {}) };
  }));
}
