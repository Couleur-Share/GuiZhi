import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { resolveSourcePlatform, type SourcePlatform } from '@guizhi/shared/utils/source-platforms';
import type { Receipt, ProgressItem } from '../../capture-harmony/entry/src/main/ets/core/Protocol';
import type { SentEntry, SentLogPort, SentLogLoad } from '../../capture-harmony/entry/src/main/ets/core/SentLog';

// 鸿蒙端“近期”页的数据层：发送记录（SentLog）、回执与记录的合并（HistoryView）、回执状态文案与分页规则。
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行与真机同一份客户端逻辑。
const require = createRequire(import.meta.url);
const core = '../../capture-harmony/entry/src/main/ets/core';
const sent: typeof import('../../capture-harmony/entry/src/main/ets/core/SentLog') = require(`${core}/SentLog.ts`);
const view: typeof import('../../capture-harmony/entry/src/main/ets/core/HistoryView') = require(`${core}/HistoryView.ts`);
const protocol: typeof import('../../capture-harmony/entry/src/main/ets/core/Protocol') = require(`${core}/Protocol.ts`);

const item = (index: number, status: string, error?: string): ProgressItem => (error === undefined ? { index, status } : { index, status, error });
const receipt = (requestId: string, itemCount = 1, items: ProgressItem[] | null = null, state = 'accepted', createdAt = 1_000): Receipt =>
  ({ id: `receipt-${requestId}`, requestId, createdAt, itemCount, state, progress: items === null ? null : { version: 1, items } });

// ---------- 来源标签 ----------

// Record<SourcePlatform, …> 让共享库新增平台时这里编译失败，提醒同步手机端的来源规则。
const LABELS: Record<SourcePlatform, string | null> = {
  douyin: '抖音', bilibili: '哔哩哔哩', xiaohongshu: '小红书', youtube: 'YouTube', v2ex: 'V2EX', nga: 'NGA',
  linuxdo: 'LINUX DO', nodeseek: 'NodeSeek', appinn: '小众软件', twolibra: '2Libra', wechat: '微信公众号', web: null, local: null,
};
const SAMPLES: string[] = [
  'https://www.douyin.com/video/7300000000000000000', 'https://v.douyin.com/abcdefg/', 'https://www.iesdouyin.com/share/video/1',
  'https://www.bilibili.com/video/BV1xx411c7mD', 'https://b23.tv/abc123',
  'https://www.xiaohongshu.com/explore/65a0000000000000', 'http://xhslink.com/a/AbCdEf', 'https://xhslink.cn/o/AbCdEf',
  'https://www.youtube.com/watch?v=dQw4w9WgXcQ', 'https://youtu.be/dQw4w9WgXcQ',
  'https://www.v2ex.com/t/1000000', 'https://bbs.nga.cn/read.php?tid=1', 'https://linux.do/t/topic/1',
  'https://www.nodeseek.com/post-1-1', 'https://meta.appinn.net/t/topic/1', 'https://2libra.com/post/node/AbC1234',
  'https://mp.weixin.qq.com/s/abcdef', 'https://example.com/a', 'https://blog.example.org/post',
];

test('来源标签与桌面端平台归类一致：已知平台用同一展示名，其余显示域名', () => {
  for (const url of SAMPLES) {
    const platform = resolveSourcePlatform('url', url);
    assert.ok(platform, `${url} 应能归类`);
    const expected = LABELS[platform];
    assert.equal(sent.sourceLabel(url), expected ?? new URL(url).hostname.replace(/^www\./, ''), `${url} → ${platform}`);
  }
});

test('来源标签：子域与大小写、端口、userinfo 不影响归类；后缀碰撞不算平台；IPv6 保留方括号；超长域名截断', () => {
  assert.equal(sent.sourceLabel('https://User:pw@V.DOUYIN.com:8443/x'), '抖音');
  assert.equal(sent.sourceLabel('HTTP://Live.Bilibili.COM/1'), '哔哩哔哩');
  for (const url of ['https://evildouyin.com/x', 'https://notlinux.do/x', 'https://douyin.com.evil.net/x']) {
    assert.equal(sent.sourceLabel(url), new URL(url).hostname, `${url} 不是平台域名`);
  }
  assert.equal(sent.sourceLabel('https://www.example.com/x'), 'example.com', '去掉 www.');
  assert.equal(sent.sourceLabel('http://[::1]:8080/x'), '[::1]');
  const long = `${'a'.repeat(40)}.example.com`;
  assert.equal(sent.sourceLabel(`https://${long}/x`), `${'a'.repeat(24)}…`);
  assert.equal(sent.sourceLabel('https://192.168.1.10:3000/x'), '192.168.1.10');
});

test('一组链接的来源：全部相同用该来源，混合来源统一称呼，没有链接为空', () => {
  assert.equal(sent.platformOf([]), '');
  assert.equal(sent.platformOf(['https://v.douyin.com/a', 'https://www.douyin.com/b']), '抖音');
  assert.equal(sent.platformOf(['https://v.douyin.com/a', 'https://b23.tv/b']), sent.MIXED_SOURCES);
  assert.equal(sent.platformOf(['https://example.com/a', 'https://www.example.com/b']), 'example.com', 'www 与裸域名归为同一来源');
});

// ---------- 发送记录的生成、追加与持久化 ----------

test('生成记录：链接内容保存链接与来源，条数取自回执；文字内容不保存链接', () => {
  const urls = 'https://v.douyin.com/a\nhttps://v.douyin.com/b\n\nhttps://v.douyin.com/a';
  const entry = sent.buildSentEntry(urls, 'auto', receipt('r1', 2), 5_000);
  assert.equal(entry.requestId, 'r1');
  assert.equal(entry.itemCount, 2);
  assert.equal(entry.sentAt, 5_000);
  assert.deepEqual(entry.urls, ['https://v.douyin.com/a', 'https://v.douyin.com/b'], '按识别规则去重');
  assert.equal(entry.platform, '抖音');
  assert.equal(entry.preview, 'https://v.douyin.com/a');

  const note = sent.buildSentEntry('今天读到一句话：https://example.com/a 很有意思', 'text', receipt('r2'), 6_000);
  assert.deepEqual(note.urls, [], '文字模式是笔记，不是待抓取的来源');
  assert.equal(note.platform, '');
  assert.equal(note.preview, '今天读到一句话：https://example.com/a 很有意思');

  const plain = sent.buildSentEntry('随手记一条想法', 'auto', receipt('r3'), 7_000);
  assert.deepEqual(plain.urls, []);
  assert.equal(plain.platform, '');
});

test('生成记录：混合内容保留候选链接与来源；预览取首个非空行、合并空白并按字数截断', () => {
  const mixed = sent.buildSentEntry('推荐这篇 https://www.bilibili.com/video/BV1 和 https://b23.tv/x', 'auto', receipt('r1'), 1);
  assert.deepEqual(mixed.urls, ['https://www.bilibili.com/video/BV1', 'https://b23.tv/x']);
  assert.equal(mixed.platform, '哔哩哔哩');
  const long = sent.buildSentEntry(`\n\n   第一行\t有   多余空白  \n第二行${'长'.repeat(100)}`, 'text', receipt('r2'), 1);
  assert.equal(long.preview, '第一行 有 多余空白');
  const cut = sent.buildSentEntry('长'.repeat(100), 'text', receipt('r3'), 1);
  assert.equal(cut.preview, `${'长'.repeat(60)}…`);
  // 预览长度是设计值 60 字：恰好 60 字原样保留，多 1 字才截断（用字面量而不是常量，改动预览长度须同时评估卡片布局）。
  assert.equal(sent.buildSentEntry('长'.repeat(60), 'text', receipt('r4'), 1).preview, '长'.repeat(60));
  assert.equal(sent.buildSentEntry('长'.repeat(61), 'text', receipt('r5'), 1).preview, `${'长'.repeat(60)}…`);
});

const entryOf = (requestId: string, sentAt = 1): SentEntry => sent.buildSentEntry(`内容 ${requestId}`, 'text', receipt(requestId), sentAt);

test('追加记录：同一编号只保留最新一条并排到最后；超过上限丢弃最早的；不修改传入数组', () => {
  const first = [entryOf('a', 1), entryOf('b', 2)];
  const retry = sent.appendSent(first, entryOf('a', 3));
  assert.deepEqual(retry.map(entry => entry.requestId), ['b', 'a']);
  assert.equal(retry[1].sentAt, 3);
  assert.deepEqual(first.map(entry => entry.requestId), ['a', 'b'], '原数组不变');

  // 上限是设计值 200 条（每次发送都会整体重写本机存储，改动上限须同时评估写入成本），这里用字面量卡住边界。
  let entries: SentEntry[] = [];
  for (let i = 0; i < 200; i++) entries = sent.appendSent(entries, entryOf(`r${i}`, i));
  assert.equal(entries.length, 200, '恰好到上限时全部保留');
  assert.equal(entries[0].requestId, 'r0');
  entries = sent.appendSent(entries, entryOf('r200', 200));
  assert.equal(entries.length, 200, '第 201 条进来时丢弃最早的一条');
  assert.equal(entries[0].requestId, 'r1');
  for (let i = 201; i < 205; i++) entries = sent.appendSent(entries, entryOf(`r${i}`, i));
  assert.equal(entries.length, 200);
  assert.equal(entries[0].requestId, 'r5', '共丢弃了最早的 5 条');
  assert.equal(entries[entries.length - 1].requestId, 'r204');
});

test('持久化：序列化再读取保持内容；首次使用（空字符串）不算损坏', () => {
  const entries = [sent.buildSentEntry('https://v.douyin.com/a\nhttps://b23.tv/b', 'auto', receipt('r1', 2), 9), entryOf('r2', 10)];
  const loaded = sent.parseSent(sent.serializeSent(entries));
  assert.equal(loaded.damaged, false);
  assert.deepEqual(JSON.parse(JSON.stringify(loaded.entries)), JSON.parse(JSON.stringify(entries)));
  const fresh = sent.parseSent('');
  assert.equal(fresh.damaged, false);
  assert.deepEqual(fresh.entries, []);
});

test('读取损坏或异常内容：整体无法识别则按损坏处理并清空，个别坏条目单独丢弃，超上限按上限修剪', () => {
  for (const raw of ['{不是 JSON', 'null', '5', '"text"', '[]', '{"version":2,"entries":[]}', '{"version":1}', '{"version":1,"entries":{}}']) {
    const loaded = sent.parseSent(raw);
    assert.equal(loaded.damaged, true, `${raw} 应视为损坏`);
    assert.deepEqual(loaded.entries, []);
  }
  const good = entryOf('ok');
  const bad: unknown[] = [null, 5, 'x', {}, { ...good, requestId: '' }, { ...good, requestId: 7 }, { ...good, preview: 1 }, { ...good, platform: null },
    { ...good, itemCount: '1' }, { ...good, sentAt: undefined }, { ...good, urls: 'https://a' }, { ...good, urls: ['https://a', 3] }];
  const loaded = sent.parseSent(JSON.stringify({ version: 1, entries: [...bad, good] }));
  assert.equal(loaded.damaged, false, '个别坏条目不算整体损坏');
  assert.deepEqual(loaded.entries.map(entry => entry.requestId), ['ok']);

  const many = Array.from({ length: 220 }, (_, i) => entryOf(`r${i}`, i));
  const trimmed = sent.parseSent(JSON.stringify({ version: 1, entries: many }));
  assert.equal(trimmed.entries.length, 200);
  assert.equal(trimmed.entries[0].requestId, 'r20');
});

class MemoryPort implements SentLogPort {
  saves: SentEntry[][] = [];
  failLoad: Error | null = null;
  failSave: Error | null = null;
  constructor(private initial: SentLogLoad = new sent.SentLogLoad()) {}
  async load(): Promise<SentLogLoad> { if (this.failLoad) throw this.failLoad; return this.initial; }
  async save(entries: SentEntry[]): Promise<void> { if (this.failSave) throw this.failSave; this.saves.push(entries.slice()); }
}
const tick = () => new Promise<void>(resolve => setImmediate(resolve));

test('SentLog：打开时读取已保存的记录；读取失败或数据损坏不抛出，只留下说明并从空记录开始', async () => {
  const stored = new sent.SentLogLoad();
  stored.entries = [entryOf('a')];
  const normal = new sent.SentLog(new MemoryPort(stored));
  await normal.open();
  assert.deepEqual(normal.snapshot().map(entry => entry.requestId), ['a']);
  assert.equal(normal.problem, '');

  const damaged = new sent.SentLogLoad();
  damaged.damaged = true;
  const broken = new sent.SentLog(new MemoryPort(damaged));
  await broken.open();
  assert.deepEqual(broken.snapshot(), []);
  assert.match(broken.problem, /无法读取.*重新开始记录/);

  const port = new MemoryPort();
  port.failLoad = new Error('存储不可用');
  const failed = new sent.SentLog(port);
  await failed.open();
  assert.deepEqual(failed.snapshot(), []);
  assert.match(failed.problem, /无法打开（存储不可用）/);
  port.failLoad = null;
  await failed.append(entryOf('later'));
  assert.deepEqual(port.saves.at(-1)?.map(entry => entry.requestId), ['later'], '打开失败后仍可继续记录');
});

test('SentLog 追加：落盘失败时抛出原因但本次运行内记录仍在，之后恢复可继续；并发追加按调用顺序串行落盘', async () => {
  const port = new MemoryPort();
  const log = new sent.SentLog(port);
  await log.open();
  port.failSave = new Error('磁盘已满');
  await assert.rejects(log.append(entryOf('a')), /磁盘已满/);
  assert.deepEqual(log.snapshot().map(entry => entry.requestId), ['a'], '内存中保留，近期页本次运行内可用');
  port.failSave = null;
  await Promise.all([log.append(entryOf('b')), log.append(entryOf('c')), log.append(entryOf('a', 9))]);
  assert.deepEqual(port.saves.map(entries => entries.map(entry => entry.requestId).join(',')), ['a,b', 'a,b,c', 'b,c,a'], '每次落盘都是完整且按序的状态');
  const snapshot = log.snapshot();
  snapshot.pop();
  assert.equal(log.snapshot().length, 3, 'snapshot 是副本');
});

test('SentLog 清除：先落盘再清内存；失败时记录仍在，不谎称已清除', async () => {
  const port = new MemoryPort();
  const log = new sent.SentLog(port);
  await log.open();
  await log.append(entryOf('a'));
  port.failSave = new Error('写入失败');
  await assert.rejects(log.clear(), /写入失败/);
  assert.equal(log.snapshot().length, 1, '落盘失败时内存保持原样');
  port.failSave = null;
  await log.clear();
  assert.deepEqual(log.snapshot(), []);
  assert.deepEqual(port.saves.at(-1), []);
  await tick();
});

// ---------- 回执状态文案（拆成“状态 + 计数徽标”后与原文案等价） ----------

// 拆分前的 receiptLabel 原样保留在这里作为参照实现，证明重构没有改变任何一种回执的文案。
function legacyLabel(r: Receipt): string {
  if (r.state === 'expired') return '已过期';
  if (!r.progress || r.progress.items.length === 0) return r.state === 'received' ? '电脑已接收，等待处理' : '中转已接收，等待电脑';
  const count = (status: string) => r.progress!.items.filter(row => row.status === status).length;
  const completed = count('completed'), failed = count('failed'), duplicate = count('duplicate'), canceled = count('canceled');
  const finished = completed + failed + duplicate + canceled;
  const parts: string[] = [];
  if (completed > 0) parts.push(`成功 ${completed}`);
  if (failed > 0) parts.push(`失败 ${failed}`);
  if (duplicate > 0) parts.push(`重复 ${duplicate}`);
  if (canceled > 0) parts.push(`取消 ${canceled}`);
  const summary = parts.join(' · ');
  if (finished < r.itemCount) return summary ? `电脑处理中（${finished}/${r.itemCount}） · ${summary}` : `电脑处理中（0/${r.itemCount}）`;
  return summary ? summary : '电脑已处理';
}

function* receipts(): Generator<Receipt> {
  const statuses = ['pending', 'processing', 'completed', 'failed', 'duplicate', 'canceled'];
  const combos: string[][] = [[]];
  for (let length = 1; length <= 3; length++) {
    const previous = combos.filter(combo => combo.length === length - 1);
    for (const combo of previous) for (const status of statuses) combos.push([...combo, status]);
  }
  for (const state of ['accepted', 'received', 'expired']) {
    for (let itemCount = 1; itemCount <= 4; itemCount++) {
      yield receipt('r', itemCount, null, state);
      for (const combo of combos) yield receipt('r', itemCount, combo.map((status, index) => item(index, status)), state);
    }
  }
}

test('receiptLabel 由状态与徽标组成，对所有状态/条数/逐项结果组合与拆分前的文案逐字一致', () => {
  let checked = 0;
  for (const r of receipts()) {
    const label = protocol.receiptLabel(r);
    assert.equal(label, legacyLabel(r), JSON.stringify(r));
    const composed = [protocol.receiptStatus(r), ...protocol.receiptBadges(r).map(badge => badge.text)].filter(part => part !== '').join(' · ');
    assert.equal(label, composed);
    checked++;
  }
  assert.ok(checked > 3000, `覆盖组合数 ${checked}`);
});

test('计数徽标：只列非零项，成功/失败/重复/取消各有固定语义色；过期与无进度没有徽标', () => {
  const r = receipt('r', 5, [item(0, 'completed'), item(1, 'completed'), item(2, 'failed', 'network'), item(3, 'duplicate'), item(4, 'canceled')]);
  assert.deepEqual(protocol.receiptBadges(r).map(badge => `${badge.text}/${badge.tone}`), ['成功 2/success', '失败 1/error', '重复 1/neutral', '取消 1/neutral']);
  assert.equal(protocol.receiptStatus(r), '', '处理完且有计数时由徽标表达结果');
  assert.deepEqual(protocol.receiptBadges(receipt('r', 2, [item(0, 'completed'), item(1, 'pending')])).map(badge => badge.text), ['成功 1']);
  assert.equal(protocol.receiptStatus(receipt('r', 2, [item(0, 'completed'), item(1, 'pending')])), '电脑处理中（1/2）');
  assert.deepEqual(protocol.receiptBadges(receipt('r', 1, [item(0, 'completed')], 'expired')), []);
  assert.deepEqual(protocol.receiptBadges(receipt('r')), []);
  assert.equal(protocol.receiptStatus(receipt('r', 1, [item(0, 'pending')])), '电脑处理中（0/1）');
});

test('处理完却没有任何计数（畸形回执：条数为 0 且逐项状态未知）时保底显示“电脑已处理”，不出现空标签', () => {
  // 正常回执经 parseReceipt 校验后不会走到这里；保底文案避免卡片标签和无障碍朗读为空，与拆分前的行为一致。
  const odd = receipt('r', 0, [item(0, 'unknown')]);
  assert.equal(protocol.receiptStatus(odd), '电脑已处理');
  assert.equal(protocol.receiptLabel(odd), '电脑已处理');
  assert.equal(protocol.receiptLabel(odd), legacyLabel(odd));
});

// ---------- 回执与发送记录合并 ----------

test('合并：按请求编号关联，命中显示预览、来源与链接；未命中如实退回“N 项收集”；顺序与回执一致', () => {
  const entries = [sent.buildSentEntry('https://v.douyin.com/a\nhttps://v.douyin.com/b', 'auto', receipt('r2', 2), 1), entryOf('other')];
  const rows = [receipt('r1', 3), receipt('r2', 2), receipt('r3')];
  const items = view.mergeHistory(rows, entries);
  assert.deepEqual(items.map(row => row.receipt.requestId), ['r1', 'r2', 'r3']);
  assert.deepEqual(items.map(row => row.title), ['3 项收集', 'https://v.douyin.com/a', '1 项收集']);
  assert.deepEqual(items.map(row => row.source), ['', '抖音', '']);
  assert.deepEqual(items[1].urls, ['https://v.douyin.com/a', 'https://v.douyin.com/b']);
  assert.deepEqual(items[0].urls, []);
  assert.equal(items[1].receipt, rows[1], '回执对象原样引用，不复制');
});

test('合并：同一编号的多条记录后者覆盖前者；空输入与不修改入参', () => {
  const first = sent.buildSentEntry('旧内容', 'text', receipt('r1'), 1);
  const second = sent.buildSentEntry('新内容', 'text', receipt('r1'), 2);
  const entries = [first, second];
  assert.equal(view.mergeHistory([receipt('r1')], entries)[0].title, '新内容');
  assert.deepEqual(entries, [first, second]);
  assert.deepEqual(view.mergeHistory([], entries), []);
  assert.equal(view.mergeHistory([receipt('r1')], [])[0].title, '1 项收集');
});

test('列表键随会影响显示的内容变化：状态推进、清除记录后标题变化都会换键，无变化则稳定', () => {
  const base = view.mergeHistory([receipt('r1', 2, [item(0, 'pending'), item(1, 'pending')])], [entryOf('r1')])[0];
  const same = view.mergeHistory([receipt('r1', 2, [item(0, 'pending'), item(1, 'pending')])], [entryOf('r1')])[0];
  assert.equal(view.historyKey(base), view.historyKey(same));
  const keys = new Set<string>([view.historyKey(base)]);
  keys.add(view.historyKey(view.mergeHistory([receipt('r1', 2, [item(0, 'completed'), item(1, 'pending')])], [entryOf('r1')])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r1', 2, [item(0, 'failed', 'network'), item(1, 'pending')])], [entryOf('r1')])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r1', 2, [item(0, 'failed', 'incomplete'), item(1, 'pending')])], [entryOf('r1')])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r1', 2, [item(0, 'pending'), item(1, 'pending')])], [])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r1', 2, [item(0, 'pending'), item(1, 'pending')], 'received')], [entryOf('r1')])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r2', 2, [item(0, 'pending'), item(1, 'pending')])], [entryOf('r2')])[0]));
  keys.add(view.historyKey(view.mergeHistory([receipt('r1')], [])[0]));
  assert.equal(keys.size, 8, '每一种可见差异都对应不同的键');
});

// ---------- 逐项失败 ----------

const failedOf = (rows: Receipt[], entries: SentEntry[]) => view.failedItems(view.mergeHistory(rows, entries)[0]);

test('逐项失败：条数一致时按序号对应链接，摘要为“第 N 项 · 域名 · 原因”，只列失败或带错误类别的项', () => {
  const input = ['https://v.douyin.com/a', 'https://www.example.com/b', 'https://b23.tv/c', 'https://blog.example.org/d'].join('\n');
  const entry = sent.buildSentEntry(input, 'urls', receipt('r1', 4), 1);
  const rows = failedOf([receipt('r1', 4, [item(0, 'completed'), item(1, 'failed', 'login_required'), item(2, 'duplicate'), item(3, 'failed', 'network')])], [entry]);
  assert.deepEqual(rows.map(row => row.summary), ['第 2 项 · www.example.com · 需在电脑登录来源网站', '第 4 项 · blog.example.org · 来源网络失败']);
  assert.deepEqual(rows.map(row => row.url), ['https://www.example.com/b', 'https://blog.example.org/d']);
  assert.deepEqual(rows.map(row => row.index), [1, 3]);
});

test('逐项失败：条数与链接数对不上（混合内容按文字计 1 项、发送后被去重）时不显示链接，宁缺毋错', () => {
  const mixed = sent.buildSentEntry('推荐 https://a.example.com/1 和 https://b.example.com/2', 'auto', receipt('r1', 1), 1);
  assert.equal(mixed.urls.length, 2);
  const rows = failedOf([receipt('r1', 1, [item(0, 'failed', 'capture_failed')])], [mixed]);
  assert.deepEqual(rows.map(row => row.summary), ['第 1 项 · 采集失败']);
  assert.equal(rows[0].url, '');
  assert.equal(rows[0].host, '');
  const none = failedOf([receipt('r1', 2, [item(0, 'failed', 'incomplete'), item(1, 'completed')])], []);
  assert.deepEqual(none.map(row => row.summary), ['第 1 项 · 内容不完整'], '没有本机记录时同样不显示链接');
});

test('逐项失败：五类错误各有说明；失败但没有类别按“采集失败”；未失败或没有进度的回执没有失败项', () => {
  const labels = ['login_required', 'rate_limited', 'network', 'capture_failed', 'incomplete'].map(code => view.itemErrorLabel(code));
  assert.deepEqual(labels, ['需在电脑登录来源网站', '来源限流', '来源网络失败', '采集失败', '内容不完整']);
  assert.equal(view.itemErrorLabel(undefined), '采集失败');
  assert.equal(view.itemErrorLabel('something_new'), '采集失败', '未知类别回退，不显示原始代码');
  const noCategory = failedOf([receipt('r1', 1, [item(0, 'failed')])], []);
  assert.deepEqual(noCategory.map(row => row.summary), ['第 1 项 · 采集失败']);
  assert.deepEqual(failedOf([receipt('r1', 2, [item(0, 'completed'), item(1, 'duplicate')])], []), []);
  assert.deepEqual(failedOf([receipt('r1')], []), []);
});

// ---------- 分页 ----------

// 模拟中转 GET /v1/history：created_at 倒序，严格早于游标，每页至多 100 条。
function relayOf(total: number) {
  const all = Array.from({ length: total }, (_, i) => receipt(`r${i}`, 1, null, 'accepted', 100_000 - i * 10));
  return { all, page: (before: number): Receipt[] => all.filter(row => before === 0 || row.createdAt < before).slice(0, view.HISTORY_PAGE_SIZE) };
}

test('分页：游标是已加载列表里最早一条的创建时间；空列表取最新一页', () => {
  assert.equal(view.historyCursor([]), 0);
  assert.equal(view.historyCursor([receipt('a', 1, null, 'accepted', 300), receipt('b', 1, null, 'accepted', 200)]), 200);
});

test('分页：从最新一页一路加载更多，取到全部且不重复、不遗漏、顺序正确；恰好整页时多一次空请求后结束', () => {
  for (const total of [0, 1, 99, 100, 101, 200, 250]) {
    const relay = relayOf(total);
    let requests = 1;
    let page = view.firstPage([], false, relay.page(0));
    while (page.more) {
      // 游标不前进时会无限翻页；设上界让错误实现表现为断言失败而不是测试挂起。
      assert.ok(requests <= Math.floor(total / view.HISTORY_PAGE_SIZE) + 1, `共 ${total} 条：翻页次数超出预期，游标可能没有前进`);
      page = view.nextPage(page.receipts, relay.page(view.historyCursor(page.receipts)));
      requests++;
    }
    assert.deepEqual(page.receipts.map(row => row.requestId), relay.all.map(row => row.requestId), `共 ${total} 条`);
    assert.equal(requests, Math.floor(total / view.HISTORY_PAGE_SIZE) + 1, `共 ${total} 条的请求次数`);
  }
});

test('刷新最新一页：不足一页时替换全部；取满一页时保留此前加载的更早记录，游标与“还有更多”状态随之保持', () => {
  const relay = relayOf(250);
  const loaded = view.nextPage(view.firstPage([], false, relay.page(0)).receipts, relay.page(relay.all[99].createdAt)); // 已加载 200 条
  assert.equal(loaded.receipts.length, 200);
  assert.equal(loaded.more, true);

  const refreshed = view.firstPage(loaded.receipts, loaded.more, relay.page(0));
  assert.deepEqual(refreshed.receipts.map(row => row.requestId), loaded.receipts.map(row => row.requestId), '翻出的旧记录不因刷新丢失');
  assert.equal(refreshed.more, true);

  // 期间新增了 3 条：最新一页整体后移，与旧数据的交界处不重复、不遗漏，总数为 203。
  const newer = Array.from({ length: 3 }, (_, i) => receipt(`n${i}`, 1, null, 'accepted', 200_000 - i));
  const grown = [...newer, ...relay.all];
  const afterGrowth = view.firstPage(loaded.receipts, loaded.more, grown.slice(0, 100));
  const ids = afterGrowth.receipts.map(row => row.requestId);
  assert.equal(new Set(ids).size, ids.length, '没有重复');
  assert.deepEqual(ids, grown.slice(0, 203).map(row => row.requestId));

  // 全部记录已不足一页（例如大多已过期）：旧的多余记录随之移除。
  const shrunk = view.firstPage(loaded.receipts, loaded.more, relay.all.slice(0, 5));
  assert.equal(shrunk.receipts.length, 5);
  assert.equal(shrunk.more, false);

  // 刚好取满一页且此前没有加载更早的记录：认为还有更多。
  const full = view.firstPage([], false, relay.page(0));
  assert.equal(full.more, true);
  assert.equal(view.firstPage(full.receipts, false, relay.page(0)).more, true, '取满一页与此前的 more 状态无关');
});

test('加载更早的一页：按编号去重，取满才继续有更多；不修改传入数组', () => {
  const relay = relayOf(150);
  const first = view.firstPage([], false, relay.page(0));
  const before = first.receipts.slice();
  const overlapping = [relay.all[99], ...relay.page(relay.all[99].createdAt)];
  const next = view.nextPage(first.receipts, overlapping);
  assert.equal(next.receipts.length, 150, '重叠的一条被去重');
  assert.equal(next.more, false, '这一页 51 条，不足一页说明已到尽头');
  assert.deepEqual(first.receipts, before);
  const empty = view.nextPage(first.receipts, []);
  assert.equal(empty.more, false);
  assert.equal(empty.receipts.length, 100);
});
