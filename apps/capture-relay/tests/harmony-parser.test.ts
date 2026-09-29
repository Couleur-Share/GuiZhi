import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { CaptureInputError, parseCaptureSubmission } from '@guizhi/shared/utils/capture-submission';
import { extractUrlsFromText as sharedExtract, isHttpUrlLike as sharedIsHttpUrlLike } from '@guizhi/shared/utils/url-text';
import type { CaptureMode } from '../../capture-harmony/entry/src/main/ets/core/Protocol';

// 鸿蒙端输入识别（CaptureParser）与中转/桌面共享实现（parseCaptureSubmission）的契约测试：
// 同一组向量分别过两边，错误码与条目必须一致。手机端据此在保存前给出与中转一致的提示。
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行同一份客户端逻辑。
const require = createRequire(import.meta.url);
const core = '../../capture-harmony/entry/src/main/ets/core';
const port: typeof import('../../capture-harmony/entry/src/main/ets/core/CaptureParser') = require(`${core}/CaptureParser.ts`);
const { captureHint }: typeof import('../../capture-harmony/entry/src/main/ets/core/CaptureHint') = require(`${core}/CaptureHint.ts`);

const MODES: CaptureMode[] = ['auto', 'urls', 'text'];
const links = (count: number, host = 'example.com') => Array.from({ length: count }, (_, i) => `https://${host}/${i}`);

interface Outcome { code: string; items: string[] }
function sharedOutcome(input: string, mode: CaptureMode): Outcome {
  try {
    const { items } = parseCaptureSubmission({ requestId: 'contract-request-0001', input, mode });
    return { code: '', items: items.map(item => `${item.kind}:${item.input}`) };
  } catch (error) {
    if (error instanceof CaptureInputError) return { code: error.message, items: [] };
    throw error;
  }
}
const portItems = (parse: ReturnType<typeof port.parseCapture>, input: string) =>
  parse.kind === 'text' ? [`text:${input}`] : parse.urls.map(url => `url:${url}`);

// 断言两边等价；返回描述文字（不一致时为原因），供模糊比对汇总。
function compare(input: string, mode: CaptureMode): string {
  const shared = sharedOutcome(input, mode), parse = port.parseCapture(input, mode);
  if (parse.kind === 'mixed') {
    // 自动模式下文字里夹着链接：共享实现按“是否有视频/论坛连接器”二选一，手机端不移植该规则，
    // 只要求共享结果恰好落在两种解释之一。
    const asText: Outcome = { code: '', items: [`text:${input}`] };
    const asUrls: Outcome = parse.linkCount > 20 ? { code: 'too_many_links', items: [] }
      : { code: '', items: parse.urls.map(url => `url:${url}`) };
    const same = (a: Outcome, b: Outcome) => a.code === b.code && JSON.stringify(a.items) === JSON.stringify(b.items);
    return same(shared, asText) || same(shared, asUrls) ? '' : `mixed 两种解释都不匹配：shared=${JSON.stringify(shared)}`;
  }
  if (parse.code !== shared.code) return `错误码不一致：port=${parse.code} shared=${shared.code}`;
  if (parse.code) return '';
  const items = portItems(parse, input);
  if (JSON.stringify(items) !== JSON.stringify(shared.items)) return `条目不一致：port=${JSON.stringify(items)} shared=${JSON.stringify(shared.items)}`;
  if (parse.itemCount !== shared.items.length) return `条数不一致：port=${parse.itemCount} shared=${shared.items.length}`;
  return '';
}
function assertEquivalent(input: string, label: string, modes: CaptureMode[] = MODES) {
  for (const mode of modes) assert.equal(compare(input, mode), '', `${label} / ${mode}`);
}

// ---------- 契约向量 ----------

interface Vector { name: string; input: string; expect?: Partial<Record<CaptureMode, { code: string; count?: number }>> }
const vectors: Vector[] = [
  { name: '单个链接', input: 'https://example.com/a', expect: { auto: { code: '', count: 1 }, urls: { code: '', count: 1 }, text: { code: '', count: 1 } } },
  { name: '首尾空白与换行', input: '  \n https://example.com/a \r\n ', expect: { auto: { code: '', count: 1 }, urls: { code: '', count: 1 } } },
  { name: '多行链接（书签导出）', input: links(3).join('\n'), expect: { auto: { code: '', count: 3 }, urls: { code: '', count: 3 } } },
  { name: '空格、制表符、全角空格、不换行空格分隔', input: 'https://a.com/1\thttps://a.com/2\u3000https://a.com/3\u00a0https://a.com/4', expect: { auto: { code: '', count: 4 } } },
  { name: '重复链接去重', input: 'https://a.com/x\nhttps://a.com/x\nhttps://a.com/y', expect: { auto: { code: '', count: 2 }, urls: { code: '', count: 2 } } },
  { name: '末尾斜杠视为不同链接（不做规范化）', input: 'https://a.com/x https://a.com/x/', expect: { auto: { code: '', count: 2 } } },
  { name: '纯文字', input: '明天买牛奶', expect: { auto: { code: '', count: 1 }, urls: { code: 'no_links' }, text: { code: '', count: 1 } } },
  { name: '含 http 字样但不是链接', input: 'http 协议和 https:// 都不是链接', expect: { auto: { code: '', count: 1 }, urls: { code: 'no_links' } } },
  { name: '空输入', input: '', expect: { auto: { code: 'empty_input' }, urls: { code: 'empty_input' }, text: { code: 'empty_input' } } },
  { name: '只有空白', input: ' \n\t \u3000 ', expect: { auto: { code: 'empty_input' }, text: { code: 'empty_input' } } },
  { name: '抖音口令（平台生成的样板文字夹链接）', input: '0.02 复制打开抖音，看看【某某的作品】 https://v.douyin.com/AbCdEf/ :3pm 12/15' },
  { name: '小红书口令（全角逗号紧贴链接）', input: 'http://xhslink.com/a/AbCdEfGh，复制本条信息，打开【小红书】App查看精彩内容！', expect: { urls: { code: '', count: 1 } } },
  { name: '用户自己写的说明夹链接', input: '明天看这个 https://example.com/read-later 很重要' },
  { name: '句末英文句号、逗号、感叹号、问号被修剪', input: '看 https://example.com/a. 还有 https://example.com/b, 以及 https://example.com/c! 和 https://example.com/d?', expect: { urls: { code: '', count: 4 } } },
  { name: '括号配平：维基百科链接保留右括号', input: 'https://en.wikipedia.org/wiki/Foo_(bar)', expect: { auto: { code: '', count: 1 } } },
  { name: '括号配平：句子括号里的链接去掉右括号', input: '（见 https://example.com/a）以及 (see https://example.com/b) 和 [https://example.com/c]', expect: { urls: { code: '', count: 3 } } },
  { name: '方括号与圆括号混合尾巴', input: 'https://example.com/a).,;' },
  { name: '引号包裹的链接', input: '"https://example.com/a" 与 “https://example.com/b” 与 `https://example.com/c`', expect: { urls: { code: '', count: 3 } } },
  { name: '大写协议与主机', input: 'HTTPS://EXAMPLE.COM/A HtTp://Example.com/B', expect: { auto: { code: '', count: 2 } } },
  { name: '带用户信息、端口、查询与片段', input: 'http://user:pass@example.com:8080/path?q=1&r=2#frag', expect: { auto: { code: '', count: 1 } } },
  { name: '空端口与多余斜杠', input: 'https://example.com:/a http:///example.com/b https:////example.com/c', expect: { urls: { code: '', count: 3 } } },
  { name: '中文域名与中文路径', input: 'http://例子.测试/路径 https://例子.com/a', expect: { urls: { code: '', count: 2 } } },
  { name: 'IPv4、IPv6 字面量', input: 'http://192.168.1.1/a https://[::1]/b http://[2001:db8::1]:8080/c http://127.1/d', expect: { urls: { code: '', count: 4 } } },
  { name: '非法主机：越界 IPv4、超范围端口、缺少主机、未闭合的 IPv6', input: 'http://999.999.999.999/a http://a:99999/b http://?x http://[::1/', expect: { urls: { code: 'no_links' } } },
  { name: '非法主机：以数字结尾的域名', input: 'http://a.1/ http://example.123/x', expect: { urls: { code: 'no_links' } } },
  { name: '20 个链接（上限）', input: links(20).join('\n'), expect: { auto: { code: '', count: 20 }, urls: { code: '', count: 20 } } },
  { name: '21 个链接', input: links(21).join('\n'), expect: { auto: { code: 'too_many_links' }, urls: { code: 'too_many_links' }, text: { code: '', count: 1 } } },
  { name: '21 行但去重后 20 个', input: [...links(20), links(1)[0]].join('\n'), expect: { auto: { code: '', count: 20 }, urls: { code: '', count: 20 } } },
  { name: '40 行只有 20 个不同链接', input: [...links(20), ...links(20)].join('\n'), expect: { urls: { code: '', count: 20 } } },
  { name: '文字里夹着 25 个链接', input: `这些都要看：${links(25).join('，')}`, expect: { urls: { code: 'too_many_links' }, text: { code: '', count: 1 } } },
  { name: '文字里夹着 25 个抖音链接', input: `看看这些 ${links(25, 'v.douyin.com').join(' ')} 就这些`, expect: { urls: { code: 'too_many_links' } } },
  { name: '混合：只有一个词不是链接', input: 'https://a.com/x foo', expect: { urls: { code: '', count: 1 } } },
  { name: '换行与 CRLF 混排的书签', input: 'https://a.com/1\r\n\r\nhttps://a.com/2\n\n\nhttps://a.com/3\r\n', expect: { auto: { code: '', count: 3 } } },
  { name: '非 http 协议不算链接', input: 'ftp://example.com/a mailto:a@b.com javascript:alert(1)', expect: { urls: { code: 'no_links' } } },
  { name: '裸的 http:// 或 https://', input: 'https://', expect: { urls: { code: 'no_links' }, auto: { code: '', count: 1 } } },
  { name: '路径中的中文与百分号', input: 'https://example.com/%E4%B8%AD%E6%96%87/路径?q=你好', expect: { auto: { code: '', count: 1 } } },
  { name: 'ASCII 恰好 32768 字节', input: 'a'.repeat(32768), expect: { auto: { code: '', count: 1 }, text: { code: '', count: 1 }, urls: { code: 'no_links' } } },
  { name: 'ASCII 32769 字节', input: 'a'.repeat(32769), expect: { auto: { code: 'input_too_large' }, urls: { code: 'input_too_large' }, text: { code: 'input_too_large' } } },
  { name: '中文恰好 32766 字节 + 2 字节', input: '汉'.repeat(10922) + 'ab', expect: { text: { code: '', count: 1 } } },
  { name: '中文 32769 字节', input: '汉'.repeat(10923), expect: { text: { code: 'input_too_large' } } },
  { name: '四字节表情恰好 32768 字节', input: '😀'.repeat(8192), expect: { text: { code: '', count: 1 } } },
  { name: '四字节表情 + 1 字节超限', input: '😀'.repeat(8192) + 'a', expect: { text: { code: 'input_too_large' } } },
  { name: '孤立代理项按替换字符（3 字节）计算', input: `${'a'.repeat(32766)}\ud800`, expect: { text: { code: 'input_too_large' } } },
  { name: '恰好 32768 字节的链接列表', input: (() => { const url = `https://example.com/${'p'.repeat(100)}`; const lines: string[] = []; let size = 0;
    while (size + url.length + 1 <= 32768) { lines.push(url + lines.length); size += url.length + String(lines.length - 1).length + 1; } return lines.join('\n').slice(0, 32768); })() },
];

test('契约向量：手机端解析与共享实现在三种模式下的错误码、条数与条目一致', () => {
  for (const vector of vectors) {
    assertEquivalent(vector.input, vector.name);
    for (const mode of MODES) {
      const expected = vector.expect?.[mode];
      if (!expected) continue;
      const parse = port.parseCapture(vector.input, mode), shared = sharedOutcome(vector.input, mode);
      // 同时钉住期望值，避免两边同步漂移而测试仍然通过。
      assert.equal(shared.code, expected.code, `${vector.name} / ${mode}：共享实现的错误码`);
      assert.equal(parse.code, expected.code, `${vector.name} / ${mode}：手机端的错误码`);
      if (expected.count !== undefined) {
        assert.equal(shared.items.length, expected.count, `${vector.name} / ${mode}：共享实现的条数`);
        assert.equal(parse.itemCount, expected.count, `${vector.name} / ${mode}：手机端的条数`);
      }
    }
  }
  assert.ok(vectors.length >= 40);
});

test('自动模式下文字夹链接：手机端只标记 mixed，共享实现按平台二选一且落在两种解释内', () => {
  const douyin = '0.02 复制打开抖音，看看【某某的作品】 https://v.douyin.com/AbCdEf/ :3pm 12/15';
  const note = '明天看这个 https://example.com/read-later 很重要';
  for (const input of [douyin, note]) {
    const parse = port.parseCapture(input, 'auto');
    assert.equal(parse.kind, 'mixed');
    assert.equal(parse.linkCount, 1);
    assert.equal(parse.itemCount, 1, 'mixed 按文字解释计为 1 条');
    assert.equal(parse.code, '');
  }
  // 共享实现：平台链接默认采集链接，普通网页默认存文字；手机端不移植这条规则。
  assert.deepEqual(sharedOutcome(douyin, 'auto').items, ['url:https://v.douyin.com/AbCdEf/']);
  assert.deepEqual(sharedOutcome(note, 'auto').items, [`text:${note}`]);
});

test('已知的刻意差异被明确钉住：百分号编码的主机名手机端视为无效（提示保守，中转仍是最终裁决）', () => {
  const input = 'http://a%41.com/x';
  assert.equal(sharedIsHttpUrlLike(input), true, '共享实现（WHATWG）会先解码主机名');
  assert.equal(port.isHttpUrlLike(input), false, '手机端不解码，按无效处理');
  assert.equal(port.parseCapture(input, 'urls').code, 'no_links');
});

// ---------- 逐函数等价与差分比对 ----------

test('extractUrlsFromText / isHttpUrlLike 与共享实现逐项一致（含边界向量）', () => {
  const inputs = vectors.map(vector => vector.input).filter(input => input.length < 4000);
  for (const input of inputs) {
    assert.deepEqual(port.extractUrlsFromText(input), sharedExtract(input), input.slice(0, 80));
    for (const token of input.split(/\s+/)) assert.equal(port.isHttpUrlLike(token), sharedIsHttpUrlLike(token), token.slice(0, 80));
  }
});

// 固定种子的确定性伪随机，保证每次运行的样本相同。
function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => { a = (a + 0x6D2B79F5) >>> 0; let t = a; t = Math.imul(t ^ (t >>> 15), t | 1); t ^= t + Math.imul(t ^ (t >>> 7), t | 61); return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
}

test('差分比对：确定性随机样本（协议、主机、端口、路径、句读、分隔符组合）与共享实现一致', () => {
  const random = mulberry32(20260929);
  const pick = <T,>(list: T[]) => list[Math.floor(random() * list.length)];
  const schemes = ['http://', 'https://', 'HTTP://', 'HtTpS://', 'ftp://', 'http:/', 'http:///', 'https:////', 'http:\\\\', ''];
  const hosts = ['example.com', 'a.b.c', 'localhost', '1.2.3.4', '1.2.3', '999.1.1.1', '0x7f.1', '0177.0.0.1', '256', '4294967295',
    '4294967296', 'a.1', 'a.1.', '1.2.3.4.5', 'a-b.com', 'a_b.com', '例子.测试', 'xn--fiq228c.com', '[::1]', '[::]', '[1:2:3:4:5:6:7:8]',
    '[1::2]', '[1::2::3]', '[::1.2.3.4]', '[1:2:3:4:5:6:1.2.3.4]', '[::ffff:999.1.1.1]', '[1:2:3:4:5:6:7:8:9]', '[::g]', '[', '[]', '',
    '1.2.3.4.0', '0.0.0.0.0', 'user:pw@example.com', 'user@', '@', 'a@b@c.com', 'a@b@', 'a@[::1]', 'a^b.com', 'a|b.com', 'A.COM', '.', '..', 'a..b', 'a{b}.com', 'a!b.com', 'a,b.com', '0x', '0X1G'];
  const ports = ['', '', '', ':80', ':', ':65535', ':65536', ':99999', ':0080', ':abc', ':8a', ':1:2'];
  const paths = ['', '/', '/a', '/a/b?c=d#e', '/wiki/Foo_(bar)', '/a)', '/a]', '/a(b', '?q=1', '#frag', '/a.', '/a,', '/a!?', '/中文路径', '/a%20b', '/a;b:c'];
  const trailing = ['', '', '.', ',', ')', ']', '!', '?', ';', ':', '，', '。', '）', '"', '”', '…', ').', '),'];
  const separators = [' ', '\n', ' ', '\t', '，', '（', '）', '：', ' ', '\u00a0', '\u3000', '\r\n', ''];
  const prefixes = ['', '', '看这个 ', '复制打开抖音，看看【标题】 ', '文字', '```', '(', '['];
  const mismatches: string[] = [];
  for (let sample = 0; sample < 6000; sample++) {
    let input = '';
    for (let part = 0, count = 1 + Math.floor(random() * 5); part < count; part++) {
      input += pick(prefixes) + pick(schemes) + pick(hosts) + pick(ports) + pick(paths) + pick(trailing) + pick(separators);
    }
    const extracted = port.extractUrlsFromText(input), expected = sharedExtract(input);
    if (JSON.stringify(extracted) !== JSON.stringify(expected)) mismatches.push(`extract ${JSON.stringify(input)}\n  port=${JSON.stringify(extracted)}\n  shared=${JSON.stringify(expected)}`);
    for (const mode of MODES) {
      const reason = compare(input, mode);
      if (reason) mismatches.push(`${mode} ${JSON.stringify(input)}: ${reason}`);
    }
    if (mismatches.length >= 8) break;
  }
  assert.deepEqual(mismatches, [], `与共享实现不一致的样本：\n${mismatches.join('\n')}`);
});

// ---------- 拆分与提示 ----------

test('splitUrlChunks：按 20 条切分，保持顺序，拼回后与原列表一致', () => {
  const urls = links(45);
  const chunks = port.splitUrlChunks(urls, 20);
  assert.deepEqual(chunks.map(chunk => chunk.length), [20, 20, 5]);
  assert.deepEqual(chunks.flat(), urls);
  assert.deepEqual(port.splitUrlChunks([], 20), []);
  assert.deepEqual(port.splitUrlChunks(links(20), 20).length, 1);
  // 每一份都能通过中转对“恰好 20 条”的校验，且以换行连接后仍被识别为同样的链接。
  for (const chunk of chunks) {
    const input = chunk.join('\n');
    assert.equal(sharedOutcome(input, 'urls').code, '');
    assert.deepEqual(sharedOutcome(input, 'urls').items.map(item => item.replace(/^url:/, '')), chunk);
    assert.equal(port.parseCapture(input, 'auto').code, '');
  }
});

test('识别提示：各状态的文案与可用操作由解析结果推导', () => {
  const hint = (input: string, mode: CaptureMode) => captureHint(port.parseCapture(input, mode), mode);
  const empty = hint('  ', 'auto');
  assert.deepEqual([empty.text, empty.canSave, empty.canSend, empty.splitParts, empty.offerModes], ['', false, false, 0, false]);

  const single = hint('https://example.com/a', 'auto');
  assert.equal(single.text, '识别到 1 个链接');
  assert.deepEqual([single.tone, single.canSave, single.canSend], ['info', true, true]);

  const text = hint('明天买牛奶', 'auto');
  assert.equal(text.text, '没有识别到链接，将作为文字整段保存');
  assert.equal(hint('明天买牛奶', 'text').text, '将作为文字整段保存');

  const mixed = hint('明天看这个 https://example.com/a 很重要', 'auto');
  assert.equal(mixed.text, '含 1 个链接，电脑将自动判断');
  assert.deepEqual([mixed.offerModes, mixed.canSave, mixed.canSend, mixed.splitParts], [true, true, true, 0]);
  assert.match(hint(`看 ${links(25).join(' ')} 就这些`.replace(/^看 /, '看：'), 'auto').text, /含 25 个链接，电脑将自动判断（按链接采集一次最多 20 个）/);

  const noLinks = hint('没有链接', 'urls');
  assert.equal(noLinks.tone, 'error');
  assert.match(noLinks.text, /没有识别到链接/);
  assert.deepEqual([noLinks.canSave, noLinks.canSend], [true, false], '可存草稿，但直接发送必被拒绝');

  const many = hint(links(45).join('\n'), 'auto');
  assert.equal(many.tone, 'warning');
  assert.equal(many.text, '共 45 个链接，一次最多 20 个');
  assert.deepEqual([many.canSave, many.canSend, many.splitParts], [false, false, 3]);
  assert.equal(hint(links(20).join('\n'), 'urls').splitParts, 0);

  const large = hint('a'.repeat(32769), 'text');
  assert.equal(large.tone, 'error');
  assert.match(large.text, /32 KiB/);
  assert.deepEqual([large.canSave, large.canSend], [false, false]);

  assert.equal(hint('a'.repeat(24576), 'text').text, '将作为文字整段保存', '未超过 75% 不显示已用量');
  assert.equal(hint('a'.repeat(30000), 'text').text, '将作为文字整段保存 · 已用 29.3 / 32 KiB');
  assert.equal(hint('a'.repeat(32768), 'text').canSave, true, '恰好 32 KiB 仍可保存');
});
