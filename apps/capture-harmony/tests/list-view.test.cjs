// 阶段 6 的列表视图：ForEach 键、预览行数映射，以及页面交给列表与浮层的布局状态。
// 只证明控制逻辑（键是否随显示字段变化、状态推导是否正确）；列表实际呈现由 Hvigor 编译和真机验证。
const test = require('node:test');
const assert = require('node:assert/strict');
const { load, listKeys } = require('./support.cjs');
const { pageWith } = require('./page-fixture.cjs');

const three = ['https://example.com/a', 'https://example.com/b', 'https://example.com/c'];
const ids = list => Array.from(list, draft => draft.requestId);
const draftOf = (over = {}) => ({ requestId: 'r1', mode: 'auto', attempted: false, lastError: '', input: 'x', createdAt: 1, ...over });

// ---------- ForEach 键 ----------

test('draftKey：草稿显示相关的状态变化（尝试过发送、失败原因、模式）都会换键，无关字段不会', () => {
  const { draftKey } = listKeys;
  const base = draftKey(draftOf());
  assert.equal(draftKey(draftOf()), base, '内容相同的两次调用得到同一个键');
  assert.notEqual(draftKey(draftOf({ attempted: true })), base, '进入“待确认”后必须重建行，否则仍显示旧状态');
  assert.notEqual(draftKey(draftOf({ lastError: '网络不可用' })), base, '发送失败原因必须让行刷新');
  assert.notEqual(draftKey(draftOf({ lastError: '网络不可用' })), draftKey(draftOf({ lastError: '连接超时' })), '换了一种失败原因同样刷新');
  assert.notEqual(draftKey(draftOf({ mode: 'text' })), base);
  assert.notEqual(draftKey(draftOf({ requestId: 'r2' })), base);
  // 原文与创建时间在同一个请求编号下不会改变（编辑会删除旧草稿、保存时生成新编号），不需要参与键。
  assert.equal(draftKey(draftOf({ input: 'y', createdAt: 9 })), base);
});

test('draftKey：字段之间有分隔，相邻字段的字符拼在一起也不会撞成同一个键', () => {
  const { draftKey } = listKeys;
  assert.notEqual(draftKey(draftOf({ requestId: 'r1', mode: 'auto' })), draftKey(draftOf({ requestId: 'r1a', mode: 'uto' })));
});

test('groupKey：成员、顺序或成员状态变化时换键，全都没变化就保持原键以便复用', () => {
  const { draftKey, groupKey } = listKeys;
  const group = (label, drafts) => ({ label, items: drafts });
  const a = draftOf({ requestId: 'a' }); const b = draftOf({ requestId: 'b' });
  const base = groupKey(group('今天', [a, b]), draftKey);
  assert.equal(groupKey(group('今天', [a, b]), draftKey), base);
  assert.notEqual(groupKey(group('昨天', [a, b]), draftKey), base, '标签不同是不同分组');
  assert.notEqual(groupKey(group('今天', [a]), draftKey), base, '删除一条后分组重建');
  assert.notEqual(groupKey(group('今天', [b, a]), draftKey), base, '顺序变化也换键');
  assert.notEqual(groupKey(group('今天', [a, draftOf({ requestId: 'b', attempted: true })]), draftKey), base, '成员状态变化换键');
  assert.notEqual(groupKey(group('今天', [a, b, draftOf({ requestId: 'c' })]), draftKey), base, '新增一条后分组重建');
  assert.equal(groupKey(group('今天', []), draftKey), '今天#', '空成员也能得到确定的键');
});

// ---------- 页面交给列表与浮层的状态 ----------

test('visibleDrafts：最新在前，延迟删除中的草稿不出现，且不会反转页面自己的 drafts', async () => {
  const { ui } = await pageWith({}, three);
  const [a, b, c] = ui.drafts;
  assert.deepEqual(ids(ui.visibleDrafts()), [c.requestId, b.requestId, a.requestId]);
  ui.hiddenIds = [b.requestId];
  assert.deepEqual(ids(ui.visibleDrafts()), [c.requestId, a.requestId]);
  assert.deepEqual(ids(ui.drafts), [a.requestId, b.requestId, c.requestId], '过滤得到新数组，反转不影响源列表');
});

test('barHidden：键盘弹出、多选中、在设置详情页时收起页签栏；别的页签上残留的详情标记不算', async () => {
  const { ui } = await pageWith({}, three);
  assert.equal(ui.barHidden(), false);
  ui.keyboardVisible = true; assert.equal(ui.barHidden(), true); ui.keyboardVisible = false;
  ui.selecting = true; assert.equal(ui.barHidden(), true); ui.selecting = false;
  ui.settingsDetail = true;
  assert.equal(ui.barHidden(), false, '不在设置页签时，残留的详情标记不应收起页签栏');
  ui.selectedTab = 3; assert.equal(ui.barHidden(), true);
  ui.settingsDetail = false; assert.equal(ui.barHidden(), false, '回到设置首页，页签栏回来');
});

test('contentEndOffset：多选栏优先，其次撤销条，都比只有页签栏时留得更多，末尾一条不会被浮层挡住', async () => {
  const layout = load('services/Layout.ets');
  const { ui } = await pageWith({}, three);
  assert.equal(ui.contentEndOffset(), layout.CONTENT_END_OFFSET);
  ui.undoMessage = '已删除 1 条草稿';
  assert.equal(ui.contentEndOffset(), layout.CONTENT_END_OFFSET_UNDO);
  ui.selecting = true;
  assert.equal(ui.contentEndOffset(), layout.CONTENT_END_OFFSET_SELECTING, '多选时撤销条不显示，按多选栏留白');
  assert.ok(layout.CONTENT_END_OFFSET_UNDO > layout.CONTENT_END_OFFSET, '撤销条叠在页签栏之上，需要更多留白');
  assert.ok(layout.CONTENT_END_OFFSET >= 64 + 12, '至少盖过悬浮页签栏（64）与间距');
});

test('配对表单：改动链接后先前解析出的服务地址失效；展开开关与设备名各自独立', async () => {
  const { ui } = await pageWith({}, three);
  ui.pairing = { origin: 'https://relay.example.com', pairingId: 'p' }; ui.pairingOrigin = 'https://relay.example.com';
  ui.editPairingText('https://other.example.com/#pair=x');
  assert.equal(ui.pairingText, 'https://other.example.com/#pair=x');
  assert.equal(ui.pairing, undefined, '旧解析结果不能再用于配对，否则会向用户没看到的地址提交');
  assert.equal(ui.pairingOrigin, '');

  assert.equal(ui.showPairingLink, false);
  ui.togglePairingLink(); assert.equal(ui.showPairingLink, true);
  ui.togglePairingLink(); assert.equal(ui.showPairingLink, false);

  ui.editDeviceName('工作手机');
  assert.equal(ui.deviceName, '工作手机');
  assert.equal(ui.pairingText, 'https://other.example.com/#pair=x', '改设备名不影响链接输入');
});
