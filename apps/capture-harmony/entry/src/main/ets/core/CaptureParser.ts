import { CaptureMode, utf8Bytes } from './Protocol';

// 手机端输入识别：等价移植 packages/shared/utils/{url-text,capture-input,capture-submission}.ts 里
// “抠链接、判定模式、条数与错误码”的规则，让编辑面板在保存前给出与中转一致的提示。
// 权威结果始终在中转与电脑端；本模块只负责提示，任何差异都以中转返回的错误码为准。
//
// 与共享实现的刻意差异（均由 apps/capture-relay/tests/harmony-parser.test.ts 的契约向量约束）：
// 1. 不使用 new URL（须同时在 Node 测试与 ArkTS 运行），改为对 authority 做主机名、端口校验；
//    百分号编码的主机名一律视为无效（WHATWG 会先解码），IDNA 映射失败不做模拟，都不在常见分享文本内。
// 2. 不移植“视频/论坛平台优先”规则：文字里夹着链接（mixed）时无法预知电脑端选择哪种解释，
//    只返回界面提示所需的信息，由用户显式选择“按链接采集 / 存为文字”。
// 本文件属于 ArkTS 严格子集：显式类型、不用解构、展开和 any。

export const MAX_ITEMS: number = 20;
export const MAX_INPUT_BYTES: number = 32768;

// 从正文里切链接的字符集：排除空白、成对包裹符与全角句读（小红书口令的逗号紧贴链接）。
const URL_IN_TEXT_RE = /https?:\/\/[^\s<>"'`，。！？；：、（）【】《》「」『』…“”‘’]+/gi;
// 半角句读跟在链接后面时属于句子而非 URL。
const TRAILING_PUNCTUATION: string = '.,;:!?';

function countChar(value: string, char: string): number {
  let total = 0;
  for (let i = 0; i < value.length; i++) {
    if (value.charAt(i) === char) total += 1;
  }
  return total;
}

// 修剪链接尾巴上的句读；括号按配平判断，维基百科 /wiki/Foo_(bar) 的右括号要留。
function trimUrlTail(value: string): string {
  let url = value;
  while (url) {
    const last = url.charAt(url.length - 1);
    const unbalanced =
      (last === ')' && countChar(url, ')') > countChar(url, '(')) ||
      (last === ']' && countChar(url, ']') > countChar(url, '['));
    if (!TRAILING_PUNCTUATION.includes(last) && !unbalanced) break;
    url = url.slice(0, -1);
  }
  return url;
}

// ---------- 主机名校验（替代 new URL） ----------

// WHATWG IPv4 数字解析：-1 表示失败；支持 0x 十六进制与前导 0 的八进制。
function parseIpv4Number(part: string): number {
  if (part === '') return -1;
  let text = part;
  let radix = 10;
  if (text.length >= 2 && (text.startsWith('0x') || text.startsWith('0X'))) {
    text = text.slice(2); radix = 16;
  } else if (text.length >= 2 && text.charAt(0) === '0') {
    text = text.slice(1); radix = 8;
  }
  if (text === '') return 0;
  const digits = radix === 16 ? /^[0-9a-fA-F]+$/ : radix === 10 ? /^[0-9]+$/ : /^[0-7]+$/;
  if (!digits.test(text)) return -1;
  return parseInt(text, radix);
}

// 最后一段是数字时，整个主机必须是合法 IPv4（例如 a.1 是非法主机）。
function endsInNumber(host: string): boolean {
  const parts = host.split('.');
  if (parts[parts.length - 1] === '') {
    if (parts.length === 1) return false;
    parts.pop();
  }
  const last = parts[parts.length - 1];
  if (last !== '' && /^[0-9]+$/.test(last)) return true;
  return parseIpv4Number(last) !== -1;
}

function ipv4Valid(host: string): boolean {
  const parts = host.split('.');
  if (parts[parts.length - 1] === '' && parts.length > 1) parts.pop();
  if (parts.length > 4) return false;
  const numbers: number[] = [];
  for (let i = 0; i < parts.length; i++) {
    const value = parseIpv4Number(parts[i]);
    if (value === -1) return false;
    numbers.push(value);
  }
  for (let i = 0; i < numbers.length - 1; i++) {
    if (numbers[i] > 255) return false;
  }
  return numbers[numbers.length - 1] < Math.pow(256, 5 - numbers.length);
}

function hostValid(host: string): boolean {
  if (host === '') return false;
  // 禁用的主机码点：控制字符、空白、< > [ ] \ ^ | 以及未解码的 %（: / ? # @ 已在切分时排除）。
  if (/[\u0000-\u0020<>\[\\\]^|%\u007f]/.test(host)) return false;
  if (endsInNumber(host)) return ipv4Valid(host);
  return true;
}

function ipv4TailValid(text: string): boolean {
  if (!/^(0|[1-9][0-9]{0,2})(\.(0|[1-9][0-9]{0,2})){3}$/.test(text)) return false;
  const parts = text.split('.');
  for (let i = 0; i < parts.length; i++) {
    if (parseInt(parts[i], 10) > 255) return false;
  }
  return true;
}

// IPv6 字面量（不含方括号）：至多一个 "::"，每段 1–4 位十六进制，末段可为点分十进制 IPv4（占两段）。
function ipv6Valid(text: string): boolean {
  if (text === '') return false;
  const compress = text.indexOf('::');
  if (compress !== text.lastIndexOf('::')) return false;
  const headText = compress === -1 ? text : text.slice(0, compress);
  const tailText = compress === -1 ? '' : text.slice(compress + 2);
  const head: string[] = headText === '' ? [] : headText.split(':');
  const tail: string[] = tailText === '' ? [] : tailText.split(':');
  const groups: string[] = head.concat(tail);
  let ipv4 = false;
  for (let i = 0; i < groups.length; i++) {
    const group = groups[i];
    if (i === groups.length - 1 && group.indexOf('.') !== -1) {
      if (!ipv4TailValid(group)) return false;
      ipv4 = true;
    } else if (!/^[0-9a-fA-F]{1,4}$/.test(group)) {
      return false;
    }
  }
  const pieces = groups.length + (ipv4 ? 1 : 0);
  return compress === -1 ? pieces === 8 : pieces <= 7;
}

// 取出 http(s):// 之后去掉 userinfo 的 host[:port]。WHATWG 对特殊协议会吞掉多余的斜杠，反斜杠等同斜杠。
function hostPortOf(url: string): string {
  const rest = url.replace(/^https?:[\\/]*/i, '');
  const end = rest.search(/[\\/?#]/);
  const authority = end === -1 ? rest : rest.slice(0, end);
  const at = authority.lastIndexOf('@');
  return at === -1 ? authority : authority.slice(at + 1);
}

// 链接的小写主机名（IPv6 保留方括号），不校验合法性；已发送记录用它归类来源与显示失败项。
export function hostOfUrl(url: string): string {
  const hostPort = hostPortOf(url);
  if (hostPort.charAt(0) === '[') {
    const close = hostPort.indexOf(']');
    return (close === -1 ? hostPort : hostPort.slice(0, close + 1)).toLowerCase();
  }
  const colon = hostPort.indexOf(':');
  return (colon === -1 ? hostPort : hostPort.slice(0, colon)).toLowerCase();
}

// 校验 http(s):// 之后的 authority（userinfo@host:port）。
function authorityValid(url: string): boolean {
  const hostPort = hostPortOf(url);
  let port = '';
  if (hostPort.charAt(0) === '[') {
    const close = hostPort.indexOf(']');
    if (close === -1) return false;
    const after = hostPort.slice(close + 1);
    if (after !== '' && after.charAt(0) !== ':') return false;
    port = after === '' ? '' : after.slice(1);
    if (!ipv6Valid(hostPort.slice(1, close))) return false;
  } else {
    const colon = hostPort.indexOf(':');
    port = colon === -1 ? '' : hostPort.slice(colon + 1);
    if (!hostValid(colon === -1 ? hostPort : hostPort.slice(0, colon))) return false;
  }
  if (port !== '') {
    if (!/^[0-9]+$/.test(port) || Number(port) > 65535) return false;
  }
  return true;
}

// 判断输入整体是否为单个 http(s) 链接（不含空白）。
export function isHttpUrlLike(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed || /\s/.test(trimmed) || !/^https?:\/\//i.test(trimmed)) return false;
  return authorityValid(trimmed);
}

// 从一段文本里按出现顺序抠出去重后的 http(s) 链接。
export function extractUrlsFromText(value: string): string[] {
  const urls: string[] = [];
  const seen = new Set<string>();
  const matches = value.match(URL_IN_TEXT_RE);
  if (matches === null) return urls;
  for (let i = 0; i < matches.length; i++) {
    const url = trimUrlTail(matches[i]);
    if (!isHttpUrlLike(url) || seen.has(url)) continue;
    seen.add(url);
    urls.push(url);
  }
  return urls;
}

function unique(values: string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (let i = 0; i < values.length; i++) {
    if (seen.has(values[i])) continue;
    seen.add(values[i]);
    result.push(values[i]);
  }
  return result;
}

// 一次解析的结果。code 与中转 POST /v1/captures 返回的 400 错误码同名；空字符串表示通过或无法在手机端判定。
export class CaptureParse {
  code: string = '';
  // urls：整段是链接列表；text：作为文字；mixed：仅自动模式，文字里夹着链接，两种解释都成立。
  kind: 'urls' | 'text' | 'mixed' = 'text';
  // 将作为独立条目的链接（去重后）；mixed 时是文字中识别到的候选链接。
  urls: string[] = [];
  linkCount: number = 0;
  // 提交后电脑会看到的条目数；mixed 时按“文字”解释计为 1。
  itemCount: number = 0;
  bytes: number = 0;
}

// 等价于中转对 (input, mode) 的校验顺序：32 KiB → 空内容 → 识别 → 无链接 → 超过 20 条。
export function parseCapture(input: string, mode: CaptureMode): CaptureParse {
  const result = new CaptureParse();
  result.bytes = utf8Bytes(input);
  if (result.bytes > MAX_INPUT_BYTES) { result.code = 'input_too_large'; return result; }
  const trimmed = input.trim();
  if (!trimmed) { result.code = 'empty_input'; return result; }
  if (mode === 'text') {
    result.kind = 'text'; result.itemCount = 1;
    return result;
  }
  if (mode === 'urls') {
    result.kind = 'urls'; result.urls = extractUrlsFromText(input);
  } else {
    const tokens = trimmed.split(/\s+/).filter((token: string): boolean => token.length > 0);
    let allUrls = tokens.length > 0;
    for (let i = 0; i < tokens.length && allUrls; i++) {
      if (!isHttpUrlLike(tokens[i])) allUrls = false;
    }
    if (allUrls) {
      result.kind = 'urls'; result.urls = unique(tokens);
    } else {
      const embedded = extractUrlsFromText(trimmed);
      if (embedded.length === 0) {
        result.kind = 'text'; result.itemCount = 1;
        return result;
      }
      result.kind = 'mixed'; result.urls = embedded; result.linkCount = embedded.length; result.itemCount = 1;
      return result;
    }
  }
  result.linkCount = result.urls.length;
  result.itemCount = result.urls.length;
  if (result.itemCount === 0) result.code = 'no_links';
  else if (result.itemCount > MAX_ITEMS) result.code = 'too_many_links';
  return result;
}

// 把链接按每份最多 size 个切开（保持原有顺序），供“按 20 条拆分保存”使用。
export function splitUrlChunks(urls: string[], size: number): string[][] {
  const chunks: string[][] = [];
  for (let i = 0; i < urls.length; i += size) {
    chunks.push(urls.slice(i, i + size));
  }
  return chunks;
}
