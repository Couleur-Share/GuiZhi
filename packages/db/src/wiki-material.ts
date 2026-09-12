import { createHash } from 'node:crypto';
import type { WikiBlock } from '@guizhi/shared/types/wiki-compiler';
export interface WikiMaterial { id: string; title: string; content: string; transcript: string | null; review_status: string; deleted_at: number | null; }
export const materialHash = (value: string) => createHash('sha256').update(value).digest('hex');
export const wikiFingerprint = (item: WikiMaterial) => materialHash(JSON.stringify([item.title, item.content, item.transcript ?? '']));

/** 段落独立寻址：前面插入段落不会挪动后续块的缓存身份。定位使用原文偏移。 */
export function wikiBlocks(item: WikiMaterial): WikiBlock[] {
  const blocks: WikiBlock[] = [], occurrences = new Map<string, number>();
  for (const field of ['content', 'transcript'] as const) {
    const text = (item[field] ?? '').replace(/\r\n/g, '\n');
    let section = '', openFence = '';
    for (const match of text.matchAll(/[^\n]+(?:\n(?!\n)[^\n]+)*/g)) {
      const paragraph = match[0], start = match.index!;
      for (let offset = 0; offset < paragraph.length;) {
        let length = Math.min(2600, paragraph.length - offset);
        // 优先在完整行边界断开，长单行才按字符切；避免切碎围栏起止标记。
        if (offset + length < paragraph.length) {
          const newline = paragraph.lastIndexOf('\n', offset + length - 1);
          if (newline > offset) length = newline - offset + 1;
        }
        const raw = paragraph.slice(offset, offset + length);
        const header = !openFence && raw.match(/^#{1,6}\s+(.+)$/m); if (header) section = header[1];
        let prefix = openFence ? `${openFence}\n` : '';
        for (const line of raw.split('\n')) {
          const fence = line.match(/^ {0,3}(`{3,}|~{3,})([^\n]*)$/);
          if (fence) {
            const marker = openFence.match(/^[`~]+/)?.[0];
            if (!marker) openFence = fence[1].slice(0, 128) + fence[2].slice(0, 64);
            else if (fence[1][0] === marker[0] && fence[1].length >= marker.length && !fence[2].trim()) openFence = '';
          }
        }
        // 合成围栏仅保障输入语法，不计入来源覆盖偏移。
        const suffix = openFence ? `\n${openFence.match(/^[`~]+/)![0]}` : '';
        if (prefix.length + raw.length + suffix.length > 3000) prefix = prefix.slice(0, 100);
        const body = `${prefix}${raw}${suffix}`;
        const hash = materialHash(JSON.stringify([item.title, field, section, body]));
        const occurrence = occurrences.get(hash) ?? 0; occurrences.set(hash, occurrence + 1);
        blocks.push({ key: `${hash}:${occurrence}`, hash, field, section, start: start + offset, end: start + offset + raw.length, text: body });
        offset += raw.length;
      }
    }
  }
  if (!blocks.length && item.title.trim()) for (let start = 0; start < item.title.length; start += 3000) {
    const text = item.title.slice(start, start + 3000), hash = materialHash(text);
    blocks.push({ key: `${hash}:${start}`, hash, field: 'title', section: '', start, end: start + text.length, text });
  }
  return blocks;
}
