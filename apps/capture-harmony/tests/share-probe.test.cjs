const test = require('node:test');
const assert = require('node:assert/strict');
const { shareProbe } = require('./support.cjs');

// 分享入口 spike（仅 dev 包）的诊断整理逻辑。
// 这里证明：只记录形状不记录内容、按现有逻辑如实预测处理结果、技术侧判据、JSONL 存取与报告。
// 扩展本身（ShareExtensionAbility、Asset Store、网络）只能靠 hvigor 编译与真机验证，不在此模拟。
const {
  SHARE_PROBE_LIMIT, ProbeStep, ShareRecordInput, safeToken, shapeOfRecord, newShareEntry, newMainEntry,
  parseEntries, upsertEntries, technicalVerdict, processNote, formatReport, panelLines,
} = shareProbe;

const SECRET = 'SECRET-BODY-9f3a';
const step = (state, detail = '') => new ProbeStep(state, detail);
const record = (fields = {}) => Object.assign(new ShareRecordInput(), fields);
const linkRecord = (url = 'https://example.com/a') => record({ utd: 'general.hyperlink', content: url });
// 被测模块在 VM 里创建数组，与宿主的数组原型不同；断言前统一拷成宿主数组。
const idsOf = text => Array.from(parseEntries(text).entries, entry => entry.id);

// 一次分享：默认探测项都成功，可按需覆盖。
function share(at, pid, caller, records, probes = {}) {
  const entry = newShareEntry(at, pid, caller, 'ReasonMessage_SystemShare', ['ohos.aafwk.param.callerBundleName'], records);
  entry.assetRw = probes.assetRw || step('ok');
  entry.credential = probes.credential || step('ok');
  entry.network = probes.network || step('ok');
  entry.elapsedMs = 321;
  return entry;
}

// ---------- 形状：只保留长度与类别 ----------

test('记录形状只含长度与类别：各字段长度、缩略图字节数、uri 协议头', () => {
  const shape = shapeOfRecord(record({
    utd: 'general.plain-text', content: '一二三四五', title: '标题', description: '描述描述描述',
    uri: 'https://example.com/a', thumbnailBytes: 2048, thumbnailUri: 'file://x/y',
  }));
  assert.equal(shape.utd, 'general.plain-text');
  assert.equal(shape.contentLength, 5);
  assert.equal(shape.titleLength, 2);
  assert.equal(shape.descriptionLength, 6);
  assert.equal(shape.uriLength, 21);
  assert.equal(shape.uriScheme, 'https');
  assert.equal(shape.thumbnailBytes, 2048);
  assert.equal(shape.thumbnailUriLength, 10);
  // 形状对象上不应有任何可以还原内容的字段。
  assert.deepEqual(Object.keys(shape).sort(), [
    'contentLength', 'descriptionLength', 'thumbnailBytes', 'thumbnailUriLength', 'titleLength', 'uriLength', 'uriScheme', 'utd',
  ]);
});

test('缺失字段长度为 0，uri 协议头逐类验证：无、file、http(s) 大小写、自定义协议、无协议、超长协议', () => {
  const empty = shapeOfRecord(record({ utd: 'general.image' }));
  assert.equal(empty.contentLength + empty.titleLength + empty.descriptionLength + empty.uriLength, 0);
  assert.equal(empty.uriScheme, 'none');
  const rows = [
    ['file://media/Photo/12/IMG.jpg', 'file'],
    ['https://example.com/a?token=x', 'https'],
    ['HTTP://EXAMPLE.COM', 'http'],
    ['datashare:///media/x', 'datashare'],
    ['/data/storage/el2/x', 'other'],
    ['没有协议', 'other'],
    ['abcdefghijklmnopqrstu:x', 'other'],
  ];
  for (const [uri, scheme] of rows) assert.equal(shapeOfRecord(record({ utd: 'general.image', uri })).uriScheme, scheme, uri);
});

test('utd 与来源等自由文本先过白名单：含空格、符号、过长或为空一律替换，不会把任意内容带进报告', () => {
  assert.equal(safeToken('general.plain-text', 'x'), 'general.plain-text');
  assert.equal(safeToken('com.tencent.mm', 'x'), 'com.tencent.mm');
  for (const bad of ['bad utd!', '', '中文', 'a'.repeat(81), 'a;b']) assert.equal(safeToken(bad, 'fallback'), 'fallback', bad);
  assert.equal(shapeOfRecord(record({ utd: 'bad utd!' })).utd, 'invalid');
  assert.equal(shapeOfRecord(record({ utd: '' })).utd, 'invalid');
});

test('隐私：条目、报告与面板摘要里都不出现正文、标题、描述、链接和缩略图地址', () => {
  const entry = share(1000, 42, 'com.tencent.mm', [record({
    utd: 'general.plain-text', content: `看看这个 https://example.com/p?token=${SECRET}`, title: SECRET, description: SECRET,
    uri: `https://example.com/${SECRET}`, thumbnailUri: `file://x/${SECRET}`,
  })]);
  const main = newMainEntry(900, 7);
  const output = [JSON.stringify(entry), formatReport('Test Phone', [main, entry], 0, 2000), panelLines(entry).join('\n')].join('\n');
  for (const leaked of [SECRET, 'example.com', '看看', 'token=']) assert.equal(output.includes(leaked), false, `不应出现 ${leaked}`);
});

test('want 键名与来源、原因：只保留安全字符，最多 30 个并排序，值一概不记录', () => {
  const keys = ['ohos.aafwk.param.callerBundleName', 'b key', '中文键', 'z.key', 'a.key', ...Array.from({ length: 40 }, (_, i) => `k${String(i).padStart(2, '0')}`)];
  const entry = newShareEntry(1, 2, 'com bad', 'a b', keys, []);
  assert.equal(entry.caller, 'unknown');
  assert.equal(entry.reason, '');
  const kept = Array.from(entry.wantKeys);
  assert.equal(kept.length, 30);
  assert.deepEqual(kept, [...kept].sort());
  assert.equal(kept.includes('b key'), false);
  assert.equal(kept.includes('中文键'), false);
  assert.ok(kept.includes('ohos.aafwk.param.callerBundleName'));
  const ok = newShareEntry(1, 2, 'com.tencent.mm', 'ReasonMessage_SystemShare', [], []);
  assert.equal(ok.caller, 'com.tencent.mm');
  assert.equal(ok.reason, 'ReasonMessage_SystemShare');
  assert.equal(ok.id, '1-2');
  assert.equal(ok.kind, 'share');
});

// ---------- 现有逻辑会怎样处理这些分享 ----------

test('用应用现有的 pickShareText / parseCapture 预测处理结果：链接、夹链接的文字、纯文字、图片、混合、空', () => {
  const mixedText = '看这个 https://example.com/a 不错';
  const manyLinks = Array.from({ length: 21 }, (_, i) => `https://example.com/${i}`).join('\n');
  const rows = [
    ['浏览器只分享链接', [linkRecord()], 'ok 长度21 忽略0', 'urls 链接1 条目1'],
    ['文字里夹着链接', [record({ utd: 'general.plain-text', content: mixedText })], `ok 长度${mixedText.length} 忽略0`, 'mixed 链接1 条目1'],
    ['纯文字', [record({ utd: 'general.plain-text', content: 'hello world' })], 'ok 长度11 忽略0', 'text 链接0 条目1'],
    ['只有图片', [record({ utd: 'general.image', uri: 'file://media/Photo/1.jpg' })], 'error 暂不支持图片或文件分享，请分享链接或文字', '-'],
    ['文字加图片', [record({ utd: 'general.plain-text', content: 'hello' }), record({ utd: 'general.image', uri: 'file://x' })], 'ok 长度5 忽略1', 'text 链接0 条目1'],
    ['没有记录', [], 'error 分享内容为空', '-'],
    ['只有空白文字', [record({ utd: 'general.plain-text', content: '   ' })], 'error 分享缺少文字内容', '-'],
    ['超过 20 个链接', [record({ utd: 'general.plain-text', content: manyLinks })], `ok 长度${manyLinks.length} 忽略0`, 'urls 链接21 条目21 code=too_many_links'],
  ];
  for (const [name, records, pick, parse] of rows) {
    const entry = newShareEntry(1, 2, 'com.example.app', '', [], records);
    assert.equal(entry.pick, pick, `${name}：pick`);
    assert.equal(entry.parse, parse, `${name}：parse`);
    assert.equal(entry.records.length, records.length, `${name}：记录条数`);
  }
});

// ---------- JSON Lines 存取 ----------

test('upsertEntries：新条目追加，同 id 原地更新（探测项逐步完成时反复保存），文件以换行结尾', () => {
  const main = newMainEntry(100, 1);
  let text = upsertEntries('', main);
  assert.ok(text.endsWith('\n'));
  const first = share(200, 7, 'com.tencent.mm', [linkRecord()], { network: step('pending') });
  text = upsertEntries(text, first);
  assert.deepEqual(idsOf(text), ['main-1', '200-7']);
  // 网络探测完成后再保存一次：条数不变，内容更新。
  first.network = step('ok', 'HTTP 200');
  text = upsertEntries(text, first);
  const entries = parseEntries(text).entries;
  assert.equal(entries.length, 2);
  assert.equal(entries[1].network.state, 'ok');
  assert.equal(entries[1].network.detail, 'HTTP 200');
  // 主进程基线同 pid 重复打开只占一条。
  text = upsertEntries(text, newMainEntry(500, 1));
  const again = parseEntries(text).entries;
  assert.equal(again.length, 2);
  assert.equal(again[0].at, 500);
});

test('upsertEntries 只保留最近 SHARE_PROBE_LIMIT 条，且可自定义上限', () => {
  assert.equal(SHARE_PROBE_LIMIT, 60);
  let text = '';
  for (let i = 0; i < 65; i++) text = upsertEntries(text, share(1000 + i, i, 'com.example.app', []));
  const entries = parseEntries(text).entries;
  assert.equal(entries.length, 60);
  assert.equal(entries[0].id, '1005-5');
  assert.equal(entries[59].id, '1064-64');
  const small = upsertEntries(upsertEntries(upsertEntries('', newMainEntry(1, 1), 2), newMainEntry(2, 2), 2), newMainEntry(3, 3), 2);
  assert.deepEqual(idsOf(small), ['main-2', 'main-3']);
});

test('损坏的行不让读取失败：截断、缺 id、未知类型、null、数组都被跳过并计数，重写时被清掉', () => {
  const good = JSON.stringify(newMainEntry(100, 1));
  const text = [good, '{"id":"x"', '{"id":"","kind":"share"}', '{"id":"y","kind":"weird"}', 'null', '[]', '', '   '].join('\n');
  const parsed = parseEntries(text);
  assert.deepEqual(idsOf(text), ['main-1']);
  assert.equal(parsed.skipped, 5);
  const rewritten = upsertEntries(text, newMainEntry(200, 2));
  assert.equal(parseEntries(rewritten).skipped, 0);
  assert.deepEqual(idsOf(rewritten), ['main-1', 'main-2']);
  assert.equal(parseEntries('').entries.length, 0);
});

// ---------- 技术侧判据 ----------

test('技术侧结论：只看读凭证与访问网络，二者都成功才是 ready；任何失败即 blocked；未配对或未完成只算证据不足；Asset Store 读写只作参考', () => {
  const assetNote = '参考：扩展内 Asset Store 读写失败：add 错误码 201；判据只看凭证读取与网络';
  const rows = [
    ['全部成功', {}, 'ready', []],
    // 计划的判据是“凭证与网络可用”：扩展只读凭证，写入发生在主应用，所以读写探测失败不能把结论拉成 blocked。
    ['Asset Store 失败但凭证与网络正常', { assetRw: step('fail', 'add 错误码 201') }, 'ready', [assetNote]],
    ['Asset Store 失败无详情', { assetRw: step('fail') }, 'ready', ['参考：扩展内 Asset Store 读写失败：无详情；判据只看凭证读取与网络']],
    ['Asset Store 未完成不影响结论', { assetRw: step('pending') }, 'ready', []],
    ['Asset Store 跳过不影响结论', { assetRw: step('skipped') }, 'ready', []],
    ['Asset Store 与读凭证都失败：判据原因在前，参考在后', { assetRw: step('fail', 'add 错误码 201'), credential: step('fail', 'c') }, 'blocked', [
      '扩展内读取配对凭证失败：c', assetNote]],
    ['未配对时 Asset Store 失败仍是证据不足', { assetRw: step('fail', 'add 错误码 201'), credential: step('none') }, 'unknown', [
      '主应用尚未配对，无法验证凭证读取；请先在 Dev 包完成配对再分享', assetNote]],
    ['读凭证失败', { credential: step('fail', '无法读取系统安全存储，请解锁手机后重试') }, 'blocked', ['扩展内读取配对凭证失败：无法读取系统安全存储，请解锁手机后重试']],
    ['网络失败', { network: step('fail', '网络超时或中断，8003 ms') }, 'blocked', ['扩展内访问中转失败：网络超时或中断，8003 ms']],
    ['失败无详情', { network: step('fail') }, 'blocked', ['扩展内访问中转失败：无详情']],
    ['未配对', { credential: step('none') }, 'unknown', ['主应用尚未配对，无法验证凭证读取；请先在 Dev 包完成配对再分享']],
    ['网络未完成', { network: step('pending') }, 'unknown', ['扩展内访问中转尚未得出结果（pending）']],
    ['失败优先于证据不足', { credential: step('none'), network: step('fail', 'x') }, 'blocked', [
      '主应用尚未配对，无法验证凭证读取；请先在 Dev 包完成配对再分享', '扩展内访问中转失败：x']],
  ];
  for (const [name, probes, state, reasons] of rows) {
    const verdict = technicalVerdict([share(1, 2, 'com.example.app', [linkRecord()], probes)]);
    assert.equal(verdict.state, state, name);
    assert.deepEqual(Array.from(verdict.reasons), reasons, name);
  }
});

test('技术侧结论只看最近一次分享；没有分享记录时给出操作提示', () => {
  const none = technicalVerdict([newMainEntry(1, 1)]);
  assert.equal(none.state, 'unknown');
  assert.match(none.reasons[0], /尚无分享记录/);
  const older = share(1, 2, 'com.example.app', [], { network: step('fail', 'x') });
  const newer = share(2, 3, 'com.example.app', []);
  assert.equal(technicalVerdict([older, newer]).state, 'ready');
  assert.equal(technicalVerdict([newer, older]).state, 'blocked');
});

test('扩展进程：缺基线、与主进程 pid 相同、独立进程（列出全部基线 pid）', () => {
  const s = share(10, 300, 'com.example.app', []);
  assert.equal(processNote([]), '尚无分享记录');
  assert.equal(processNote([newMainEntry(1, 100)]), '尚无分享记录');
  assert.match(processNote([s]), /缺少主进程基线/);
  assert.equal(processNote([newMainEntry(1, 300), s]), '与主进程相同（pid 300）');
  assert.equal(processNote([newMainEntry(1, 100), newMainEntry(2, 200), s]), '独立进程（扩展 pid 300，主进程基线 pid 100、200）');
});

// ---------- 报告与面板摘要 ----------

test('报告：环境、判据速览、按来源汇总的字段出现次数、原始 JSON Lines 都齐全（逐行精确匹配）', () => {
  const main = newMainEntry(900, 100);
  main.assetRw = step('ok'); main.credential = step('ok'); main.network = step('ok', '已访问中转');
  const chat = () => [record({ utd: 'general.plain-text', content: 'x'.repeat(30), title: 'abcde' })];
  // 同一来源两次形状相同的分享：现有逻辑的预测结果要去重，不能重复罗列。
  const wechat1 = share(1000, 300, 'com.tencent.mm', chat());
  const wechat2 = share(1500, 301, 'com.tencent.mm', chat());
  const browser1 = share(2000, 302, 'com.huawei.hmos.browser', [record({ utd: 'general.hyperlink', content: 'https://example.com/a', title: 'abcd' })]);
  const browser2 = share(3000, 303, 'com.huawei.hmos.browser', [
    record({ utd: 'general.hyperlink', content: 'https://example.com/b', description: 'desc', uri: 'https://example.com/b' }),
    record({ utd: 'general.image', uri: 'file://x', thumbnailBytes: 10 }),
    record({ utd: 'general.image', thumbnailUri: 'file://t' }),
    record({ utd: 'general.plain-text', content: 'x' }),
  ]);
  const now = Date.UTC(2026, 8, 29, 4, 5, 6);
  const report = formatReport('Test Phone · HarmonyOS 6 · API 23 · 归知 0.5.1', [main, wechat1, wechat2, browser1, browser2], 0, now);
  const lines = report.split('\n');
  for (const expected of [
    '# 归知 分享诊断报告',
    '生成时间：2026-09-29T04:05:06.000Z',
    '设备：Test Phone · HarmonyOS 6 · API 23 · 归知 0.5.1',
    '记录：分享 4 条，主进程基线 1 条',
    '- 结论：技术侧可行（扩展内可读取配对凭证并访问中转）',
    '- 扩展进程：独立进程（扩展 pid 303，主进程基线 pid 100）',
    '- 最近一次分享：Asset Store 正常；凭证 正常；网络 正常；用时 321 ms',
    '- 主进程对照：Asset Store 正常；凭证 正常；网络 正常（已访问中转）',
    'com.tencent.mm：2 次分享，2 条记录',
    '  字段出现：content 2 · title 2 · description 0 · uri 0 · 缩略图 0',
    '  类型：general.plain-text',
    '  现有逻辑会：ok 长度30 忽略0 → text 链接0 条目1',
    'com.huawei.hmos.browser：2 次分享，5 条记录',
    // 浏览器 5 条记录：content 3（含 1 字符的短文字）、title 1、description 1、uri 2、缩略图 2（一条只有字节数、一条只有缩略图地址）。
    '  字段出现：content 3 · title 1 · description 1 · uri 2 · 缩略图 2',
    '  类型：general.hyperlink、general.image、general.plain-text',
    '  现有逻辑会：ok 长度21 忽略0；ok 长度23 忽略2 → urls 链接1 条目1；mixed 链接1 条目1',
  ]) assert.ok(lines.includes(expected), `报告缺少整行：${expected}\n${report}`);
  // 原始记录段：每条一行，可逐行解析。
  const raw = report.split('## 原始记录（JSON Lines，仅含长度与类别）\n')[1].split('\n');
  assert.equal(raw.length, 5);
  assert.deepEqual(raw.map(line => JSON.parse(line).id), ['main-100', '1000-300', '1500-301', '2000-302', '3000-303']);
});

test('报告在没有分享、探测失败、有损坏行时也给出可执行的说明（逐行精确匹配）', () => {
  const empty = formatReport('P', [], 0, 0).split('\n');
  assert.ok(empty.includes('记录：分享 0 条，主进程基线 0 条'));
  assert.ok(empty.includes('- 结论：证据不足（见下方原因）'));
  assert.ok(empty.includes('  · 尚无分享记录：请从浏览器等 App 分享一次到「归知收集 Dev」'));
  assert.ok(empty.includes('- 扩展进程：尚无分享记录'));
  assert.ok(empty.includes('（无）'));
  const failed = formatReport('P', [share(1, 2, 'com.example.app', [], {
    assetRw: step('fail', 'add 错误码 201'), network: step('fail', '网络超时或中断，8003 ms'),
  })], 2, 0).split('\n');
  assert.ok(failed.includes('记录：分享 1 条，主进程基线 0 条，2 行无法解析已跳过'));
  assert.ok(failed.includes('- 结论：技术侧受阻（见下方原因）'));
  assert.ok(failed.includes('  · 扩展内访问中转失败：网络超时或中断，8003 ms'));
  assert.ok(failed.includes('  · 参考：扩展内 Asset Store 读写失败：add 错误码 201；判据只看凭证读取与网络'));
  assert.ok(failed.includes('- 最近一次分享：Asset Store 失败（add 错误码 201）；凭证 正常；网络 失败（网络超时或中断，8003 ms）；用时 321 ms'));
  // 只有 Asset Store 读写失败时结论仍是可行，参考项照常写出。
  const assetOnly = formatReport('P', [share(1, 2, 'com.example.app', [], { assetRw: step('fail', 'add 错误码 201') })], 0, 0).split('\n');
  assert.ok(assetOnly.includes('- 结论：技术侧可行（扩展内可读取配对凭证并访问中转）'));
  assert.ok(assetOnly.includes('  · 参考：扩展内 Asset Store 读写失败：add 错误码 201；判据只看凭证读取与网络'));
  // 没有主进程基线时不输出“主进程对照”行。
  assert.equal(failed.some(line => line.startsWith('- 主进程对照')), false);
});

test('面板摘要：来源、最多 6 条记录逐条一行、现有逻辑预测与三项探测状态', () => {
  const records = Array.from({ length: 8 }, (_, i) => record({ utd: 'general.plain-text', content: 'x'.repeat(i + 1) }));
  const lines = panelLines(share(1, 2, 'com.tencent.mm', records, { network: step('pending') }));
  assert.equal(lines[0], '来源：com.tencent.mm　记录 8 条');
  assert.equal(lines[1], '1. general.plain-text · content 1 · title 0 · description 0 · uri 0(none)');
  assert.equal(lines[6], '6. general.plain-text · content 6 · title 0 · description 0 · uri 0(none)');
  assert.equal(lines.filter(line => /^\d+\. /.test(line)).length, 6);
  // pickShareText 把所有文字记录用换行连成一段。
  const joinedLength = records.map(item => item.content).join('\n').length;
  assert.deepEqual(Array.from(lines.slice(7)), [
    `现有逻辑会：ok 长度${joinedLength} 忽略0`,
    'Asset Store：正常',
    '配对凭证：正常',
    '访问中转：未完成',
  ]);
  // 其余状态的文案：无数据（未配对）、失败、跳过，带详情时括号附上。
  const other = panelLines(share(1, 2, 'com.example.app', [], {
    assetRw: step('fail', 'add 错误码 201'), credential: step('none', '未配对'), network: step('skipped'),
  }));
  assert.deepEqual(Array.from(other.slice(-3)), ['Asset Store：失败（add 错误码 201）', '配对凭证：无数据（未配对）', '访问中转：跳过']);
});
