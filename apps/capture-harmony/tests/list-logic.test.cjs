// 阶段 6 的列表逻辑：按日分组、多选集合、延迟删除与撤销、多选发送的范围。
// 只证明纯逻辑；列表、滑动手势与撤销条的实际呈现由 Hvigor 编译和真机验证。
const test = require('node:test');
const assert = require('node:assert/strict');
const { clock, settle, deferred, dayGroups, selection, pendingDelete, batchSend } = require('./support.cjs');

// 一律用本机时区的构造函数生成时间，测试结果不依赖运行机器的时区。
const local = (year, month, day, hour = 12, minute = 0) => new Date(year, month - 1, day, hour, minute).getTime();

// ---------- 按日分组 ----------

test('dayLabel：按本机日历日分为今天 / 昨天 / 更早，跨过午夜就换组而不是“24 小时内”', () => {
  const { dayLabel } = dayGroups;
  // 刚过午夜：31 分钟前的 23:59 已经是“昨天”，午夜整点是“今天”。
  const justAfterMidnight = local(2026, 9, 29, 0, 30);
  assert.equal(dayLabel(local(2026, 9, 28, 23, 59), justAfterMidnight), '昨天');
  assert.equal(dayLabel(local(2026, 9, 29, 0, 0), justAfterMidnight), '今天');
  assert.equal(dayLabel(local(2026, 9, 27, 23, 59), justAfterMidnight), '更早');
  // 同一天的上午与深夜同组。
  assert.equal(dayLabel(local(2026, 9, 29, 23, 59), local(2026, 9, 29, 8, 0)), '今天', '晚于现在（时钟偏差）按今天处理');
  // 跨月、跨年、闰日。
  assert.equal(dayLabel(local(2026, 9, 30, 20), local(2026, 10, 1, 8)), '昨天');
  assert.equal(dayLabel(local(2026, 12, 31, 23), local(2027, 1, 1, 8)), '昨天');
  assert.equal(dayLabel(local(2028, 2, 29, 20), local(2028, 3, 1, 9)), '昨天');
  assert.equal(dayLabel(local(2028, 2, 28, 20), local(2028, 3, 1, 9)), '更早');
  // 无法判断的时间归入更早，而不是抛出或显示“今天”。
  for (const bad of [0, -5, NaN, Infinity, -Infinity]) assert.equal(dayLabel(bad, local(2026, 9, 29)), '更早', String(bad));
});

test('groupByDay：固定按今天、昨天、更早排列，空组不出现，组内保持传入顺序', () => {
  const { groupByDay } = dayGroups;
  const now = local(2026, 9, 29, 15);
  const items = [
    { id: 'old1', at: local(2026, 9, 1) }, { id: 'today1', at: local(2026, 9, 29, 9) },
    { id: 'yesterday1', at: local(2026, 9, 28, 22) }, { id: 'today2', at: local(2026, 9, 29, 14) },
    { id: 'old2', at: 0 },
  ];
  const groups = groupByDay(items, item => item.at, now);
  assert.deepEqual(Array.from(groups, group => group.label), ['今天', '昨天', '更早']);
  assert.deepEqual(Array.from(groups, group => Array.from(group.items, item => item.id)), [['today1', 'today2'], ['yesterday1'], ['old1', 'old2']]);
  // 只有一天有内容时，其余空组不返回。
  const onlyOld = groupByDay([items[0]], item => item.at, now);
  assert.deepEqual(Array.from(onlyOld, group => group.label), ['更早']);
  assert.equal(groupByDay([], item => item.at, now).length, 0);
  // 不修改传入数组。
  assert.deepEqual(items.map(item => item.id), ['old1', 'today1', 'yesterday1', 'today2', 'old2']);
});

// ---------- 多选集合 ----------

test('多选集合：切换、收缩到仍存在的条目、全选判断都不修改传入数组', () => {
  const { toggleId, keepExisting, allSelected } = selection;
  const empty = [];
  const one = toggleId(empty, 'a');
  assert.deepEqual(Array.from(one), ['a']);
  assert.equal(empty.length, 0);
  const two = toggleId(one, 'b');
  assert.deepEqual(Array.from(two), ['a', 'b']);
  assert.deepEqual(Array.from(toggleId(two, 'a')), ['b']);
  assert.deepEqual(Array.from(two), ['a', 'b'], '取消选择时旧数组保持不变');
  // 条目被发送、删除后，选择集合自动收缩。
  assert.deepEqual(Array.from(keepExisting(['a', 'b', 'c'], ['c', 'a', 'z'])), ['a', 'c']);
  assert.deepEqual(Array.from(keepExisting(['a'], [])), []);
  // 全选：需要覆盖全部可选条目；没有可选条目时不算全选。
  assert.equal(allSelected(['a', 'b'], ['a', 'b']), true);
  assert.equal(allSelected(['a', 'b', 'x'], ['a', 'b']), true, '多出的已失效编号不影响判断');
  assert.equal(allSelected(['a'], ['a', 'b']), false);
  assert.equal(allSelected([], []), false);
  assert.equal(allSelected(['a'], []), false);
});

// ---------- 延迟删除 ----------

// 计时器夹具：取出并触发最早登记的一个计时器。
function fire(time) {
  const [id, timer] = time.timers.entries().next().value;
  time.timers.delete(id);
  timer.fn();
}

function harness(commit) {
  const time = clock();
  const changes = [], failures = [];
  const hooks = new pendingDelete.DeleteHooks();
  hooks.changed = (hidden, undoable) => { changes.push([Array.from(hidden), undoable]); };
  hooks.failed = list => { failures.push(Array.from(list, item => [item.requestId, item.message])); };
  const pending = new pendingDelete.PendingDeletes(commit, hooks, time.globals.setTimeout, time.globals.clearTimeout);
  return { pending, time, changes, failures };
}

test('延迟删除：立即隐藏并开启 8 秒撤销窗口；到点才真正删除，删除完成前仍保持隐藏', async () => {
  const wait = deferred();
  const commits = [];
  const { pending, time, changes } = harness(async ids => { commits.push(Array.from(ids)); await wait.promise; return []; });
  await pending.schedule(['a', 'b']);
  assert.deepEqual(Array.from(pending.hidden()), ['a', 'b']);
  assert.equal(pending.undoable(), true);
  assert.deepEqual(changes, [[['a', 'b'], true]]);
  assert.equal(commits.length, 0, '撤销窗口内不删除');
  assert.equal(time.timers.size, 1);
  assert.equal(pendingDelete.DELETE_UNDO_MS, 8000);
  assert.equal([...time.timers.values()][0].delay, 8000);

  fire(time);
  assert.deepEqual(commits, [['a', 'b']], '到点提交一次，且是这两条');
  assert.deepEqual(changes[changes.length - 1], [['a', 'b'], false], '窗口关闭，但删除完成前仍隐藏');
  assert.equal(pending.undoable(), false);
  assert.deepEqual(Array.from(pending.hidden()), ['a', 'b']);
  wait.resolve(); await settle();
  assert.deepEqual(Array.from(pending.hidden()), []);
  assert.deepEqual(changes[changes.length - 1], [[], false]);
});

test('延迟删除：撤销窗口内撤销，草稿恢复显示、不发生任何删除、计时器被清除', async () => {
  let committed = 0;
  const { pending, time, changes } = harness(async () => { committed++; return []; });
  await pending.schedule(['a', 'b', 'c']);
  assert.deepEqual(Array.from(pending.undo()), ['a', 'b', 'c']);
  assert.deepEqual(Array.from(pending.hidden()), []);
  assert.deepEqual(changes[changes.length - 1], [[], false]);
  assert.equal(time.timers.size, 0, '撤销后计时器被清除');
  assert.equal(committed, 0);
  // 没有可撤销的内容时返回空数组，也不再通知。
  const before = changes.length;
  assert.deepEqual(Array.from(pending.undo()), []);
  assert.equal(changes.length, before);
});

test('延迟删除：窗口结束后撤销无效；立即结束窗口（进入后台）会马上删除', async () => {
  const commits = [];
  const { pending, time } = harness(async ids => { commits.push(Array.from(ids)); return []; });
  await pending.schedule(['a']);
  fire(time); await settle();
  assert.deepEqual(Array.from(pending.undo()), [], '已经删除，不能再撤销');
  assert.deepEqual(commits, [['a']]);

  await pending.schedule(['b', 'c']);
  await pending.flush();
  assert.deepEqual(commits, [['a'], ['b', 'c']]);
  assert.equal(time.timers.size, 0, 'flush 后没有残留计时器');
  assert.deepEqual(Array.from(pending.hidden()), []);
  // 没有待删除项时 flush 与空 schedule 都是空操作。
  await pending.flush(); await pending.schedule([]);
  assert.equal(commits.length, 2);
  assert.equal(time.timers.size, 0);
});

test('延迟删除：再次删除会立即结束上一批的窗口；两批在删除完成前都保持隐藏，只保留一个计时器', async () => {
  const waits = [deferred(), deferred()];
  const commits = [];
  const { pending, time, changes } = harness(async ids => { commits.push(Array.from(ids)); await waits[commits.length - 1].promise; return []; });
  await pending.schedule(['a']);
  const second = pending.schedule(['b']);
  assert.deepEqual(commits, [['a']], '上一批立即提交');
  assert.deepEqual(Array.from(pending.hidden()).sort(), ['a', 'b'], '上一批删除中、新一批在窗口内，都隐藏');
  assert.equal(pending.undoable(), true);
  assert.equal(time.timers.size, 1, '只有新一批的计时器');
  // 撤销只影响新一批。
  assert.deepEqual(Array.from(pending.undo()), ['b']);
  assert.deepEqual(Array.from(pending.hidden()), ['a']);
  waits[0].resolve(); await second;
  assert.deepEqual(Array.from(pending.hidden()), []);
  assert.deepEqual(changes[changes.length - 1], [[], false]);
});

test('延迟删除：逐条记账，失败的条目重新出现并上报原因，成功的条目不受影响；整体抛出则整批记为失败', async () => {
  const partial = harness(async ids => ids.filter(id => id === 'b').map(id => {
    const failure = new pendingDelete.DeleteFailure();
    failure.requestId = id; failure.message = '写入失败';
    return failure;
  }));
  await partial.pending.schedule(['a', 'b', 'c']);
  await partial.pending.flush();
  assert.deepEqual(partial.failures, [[['b', '写入失败']]]);
  assert.deepEqual(Array.from(partial.pending.hidden()), [], '失败的 b 也不再隐藏，会重新出现在列表里');

  const thrown = harness(async () => { throw new Error('存储不可用'); });
  await thrown.pending.schedule(['x', 'y']);
  await thrown.pending.flush();
  assert.deepEqual(thrown.failures, [[['x', '存储不可用'], ['y', '存储不可用']]]);
  assert.deepEqual(Array.from(thrown.pending.hidden()), []);

  // 抛出的错误没有说明时也要有可读的原因。
  const silent = harness(async () => { throw new Error(''); });
  await silent.pending.schedule(['z']);
  await silent.pending.flush();
  assert.deepEqual(silent.failures, [[['z', '删除失败']]]);
});

test('延迟删除：待删除的编号是拷贝，调用方之后改动自己的数组不会改变将被删除的内容', async () => {
  const commits = [];
  const { pending, time } = harness(async ids => { commits.push(Array.from(ids)); return []; });
  const ids = ['a', 'b'];
  await pending.schedule(ids);
  ids.push('c'); ids[0] = 'z';
  assert.deepEqual(Array.from(pending.hidden()), ['a', 'b']);
  fire(time); await settle();
  assert.deepEqual(commits, [['a', 'b']]);
});

// ---------- 多选发送的范围 ----------

function fakeQueue(inputs) {
  const drafts = inputs.map((input, index) => ({ requestId: `id${index}`, input, mode: 'auto', attempted: false, binding: '', createdAt: index, lastError: '', lastErrorAt: 0 }));
  const sent = [];
  return {
    sent,
    snapshot: () => ({ drafts: drafts.filter(draft => !sent.includes(draft.requestId)) }),
    isSending: () => false,
    send: async requestId => { sent.push(requestId); return { id: `r-${requestId}`, requestId, createdAt: 1, itemCount: 1, state: 'accepted', progress: null }; },
  };
}

test('sendBatch 的 only：只发送指定的条目，总数按指定范围计，不存在的编号被忽略；空集合什么也不发', async () => {
  const progress = [];
  const hooks = new batchSend.BatchHooks();
  hooks.finished = (requestId, done, total) => { progress.push([requestId, done, total]); };
  const queue = fakeQueue(['一', '二', '三', '四']);
  const result = await batchSend.sendBatch(queue, 'pc', {}, hooks, ['id1', 'id3', 'nope']);
  assert.equal(result.total, 2);
  assert.deepEqual(Array.from(result.sent), ['id1', 'id3']);
  assert.deepEqual(queue.sent, ['id1', 'id3']);
  assert.deepEqual(progress, [['id1', 1, 2], ['id3', 2, 2]]);
  assert.equal(result.ok, true);

  // 空的选择集合不能被当成“全部”。
  const none = fakeQueue(['一', '二']);
  const nothing = await batchSend.sendBatch(none, 'pc', {}, new batchSend.BatchHooks(), []);
  assert.equal(nothing.total, 0);
  assert.deepEqual(none.sent, []);

  // 不传 only 时仍是全部草稿。
  const all = fakeQueue(['一', '二', '三']);
  const everything = await batchSend.sendBatch(all, 'pc', {}, new batchSend.BatchHooks());
  assert.equal(everything.total, 3);
  assert.deepEqual(all.sent, ['id0', 'id1', 'id2']);
});
