// 在 `hdc shell uitest dumpLayout` 导出的控件树里按文字/id 查找控件，输出可点击的中心点。
// 纯函数部分供 Node 测试；命令行：node layout-find.mjs <layout.json> <文字> [--index n] [--id] [--list]
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

// bounds 形如 "[0,0][1260,2720]"；无法解析时返回 undefined，调用方跳过该节点。
export function parseBounds(value) {
  const match = /^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$/.exec(String(value ?? '').trim());
  if (!match) return undefined;
  const [left, top, right, bottom] = match.slice(1).map(Number);
  if (right <= left || bottom <= top) return undefined;
  return { left, top, right, bottom, x: Math.round((left + right) / 2), y: Math.round((top + bottom) / 2) };
}

function attributesOf(node) {
  return node && typeof node === 'object' && node.attributes && typeof node.attributes === 'object' ? node.attributes : {};
}

function visit(node, callback) {
  if (!node || typeof node !== 'object') return;
  callback(node);
  const children = Array.isArray(node.children) ? node.children : [];
  for (const child of children) visit(child, callback);
}

// mode: 'text'（默认，匹配 text 与 description）或 'id'（匹配 id 与 key）。
// 精确匹配优先于包含匹配；同级别内保持控件树顺序，屏幕外的控件排在屏幕内之后。
export function findNodes(layout, query, mode = 'text') {
  const wanted = String(query ?? '');
  if (!wanted) return [];
  const screen = parseBounds(attributesOf(layout).bounds);
  const exact = [];
  const partial = [];
  visit(layout, node => {
    const attributes = attributesOf(node);
    const bounds = parseBounds(attributes.bounds);
    if (!bounds) return;
    const fields = mode === 'id' ? [attributes.id, attributes.key] : [attributes.text, attributes.description];
    const values = fields.map(value => String(value ?? '')).filter(value => value.length > 0);
    const hit = {
      text: String(attributes.text ?? ''), description: String(attributes.description ?? ''),
      id: String(attributes.id ?? ''), key: String(attributes.key ?? ''), type: String(attributes.type ?? ''),
      clickable: attributes.clickable === 'true', enabled: attributes.enabled !== 'false',
      bounds: attributes.bounds, x: bounds.x, y: bounds.y,
      onScreen: !screen || (bounds.x >= screen.left && bounds.x <= screen.right && bounds.y >= screen.top && bounds.y <= screen.bottom),
    };
    if (values.some(value => value === wanted)) exact.push(hit);
    else if (mode === 'text' && values.some(value => value.includes(wanted))) partial.push(hit);
  });
  const byVisibility = list => [...list.filter(item => item.onScreen), ...list.filter(item => !item.onScreen)];
  return [...byVisibility(exact), ...byVisibility(partial)];
}

// 按控件树顺序列出屏幕上可见的文字（去重），便于不看截图也能确认界面内容。
export function listTexts(layout) {
  const seen = new Set();
  const texts = [];
  visit(layout, node => {
    const attributes = attributesOf(node);
    if (!parseBounds(attributes.bounds)) return;
    for (const value of [attributes.text, attributes.description]) {
      const text = String(value ?? '').trim();
      if (text && !seen.has(text)) { seen.add(text); texts.push(text); }
    }
  });
  return texts;
}

function main(argv) {
  const args = argv.slice(2);
  if (args[0] === '--texts') {
    if (!args[1]) { console.error('用法：node layout-find.mjs --texts <layout.json>'); return 2; }
    for (const text of listTexts(JSON.parse(readFileSync(args[1], 'utf8')))) console.log(text);
    return 0;
  }
  const flags = new Set(args.filter(value => value === '--id' || value === '--list'));
  const indexAt = args.indexOf('--index');
  const index = indexAt >= 0 ? Number(args[indexAt + 1]) : 0;
  // 没有 --index 时不能排除任何位置参数（indexAt + 1 会等于 0，误丢第一个参数）。
  const positional = args.filter((value, at) => !value.startsWith('--') && (indexAt < 0 || at !== indexAt + 1));
  const [file, query] = positional;
  if (!file || !query || !Number.isInteger(index) || index < 0) {
    console.error('用法：node layout-find.mjs <layout.json> <文字> [--index n] [--id] [--list]');
    return 2;
  }
  const layout = JSON.parse(readFileSync(file, 'utf8'));
  const hits = findNodes(layout, query, flags.has('--id') ? 'id' : 'text');
  if (flags.has('--list')) {
    for (const hit of hits) console.log(`${hit.x} ${hit.y}\t${hit.type}\t${hit.text || hit.description || hit.id || hit.key}\t${hit.onScreen ? '' : '屏幕外'}`);
    return hits.length > 0 ? 0 : 3;
  }
  if (hits.length <= index) {
    console.error(`未找到“${query}”（匹配 ${hits.length} 个控件，索引 ${index}）`);
    return 3;
  }
  if (!hits[index].onScreen) console.error('警告：命中的控件中心在屏幕外，点击可能无效');
  console.log(`${hits[index].x} ${hits[index].y}`);
  return 0;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) process.exitCode = main(process.argv);
