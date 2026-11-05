export interface ReadingBlock { id: string; kind: string; text: string; imageId: string; level: number; }
// 原生 Text / Image 阅读；不使用 WebView，不解释 HTML，不发起远程媒体请求。
export function readingBlocks(content: string): ReadingBlock[] {
  const blocks: ReadingBlock[] = [];
  const parts = content.split(/(!\[[^\]\n]{0,500}\]\([^\)\n]{1,1024}\))/g);
  for (let i = 0; i < parts.length; i++) {
    const image = parts[i].match(/^!\[([^\]\n]*)\]\(([^\)\n]+)\)$/);
    if (image) {
      const local = image[2].match(/^guizhi-sync:\/\/([A-Za-z0-9_-]{16,64})$/);
      blocks.push({ id: String(blocks.length), kind: local ? 'image' : 'external', text: image[1] || '图片', imageId: local ? local[1] : '', level: 0 });
    } else {
      const lines = parts[i].split('\n'); let code = false;
      for (let line = 0; line < lines.length; line++) {
        if (/^\s*```/.test(lines[line])) { code = !code; continue; }
        const heading = !code ? lines[line].match(/^(#{1,6})\s+(.*)$/) : null;
        const text = heading ? heading[2] : lines[line];
        for (let offset = 0; offset < text.length; offset += 4096) {
          blocks.push({ id: String(blocks.length), kind: code ? 'code' : heading ? 'heading' : 'text',
            text: text.slice(offset, offset + 4096), imageId: '', level: heading ? heading[1].length : 0 });
        }
      }
    }
  }
  return blocks;
}
export function readingImageIds(content: string): string[] {
  const ids: string[] = [], expression = /!\[[^\]\n]{0,500}\]\(guizhi-sync:\/\/([A-Za-z0-9_-]{16,64})\)/g;
  let match: RegExpExecArray | null = expression.exec(content);
  while (match) { if (!ids.includes(match[1])) ids.push(match[1]); match = expression.exec(content); }
  return ids;
}
