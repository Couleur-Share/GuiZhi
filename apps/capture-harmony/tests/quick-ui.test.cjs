const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { load, createPage } = require('./support.cjs');

const { relativeTime } = load('core/RelativeTime.ts');
const protocol = load('core/Protocol.ts');
const { receiptLabel, receiptTone } = protocol;

// ---------- 相对时间（全部用本地时间构造，结果不依赖运行机器的时区） ----------
const at = (year, month, day, hour = 0, minute = 0, second = 0) => new Date(year, month - 1, day, hour, minute, second).getTime();

test('相对时间：分钟、小时按间隔，跨零点按日历日（昨天/前天），更早显示日期', () => {
  const now = at(2026, 9, 29, 12, 0);
  const vectors = [
    [now, '刚刚'], [now - 59000, '刚刚'], [now - 60000, '1 分钟前'], [now - 59 * 60000, '59 分钟前'],
    [at(2026, 9, 29, 11, 0), '1 小时前'], [at(2026, 9, 29, 0, 1), '11 小时前'],
    [at(2026, 9, 28, 23, 30), '昨天 23:30'], [at(2026, 9, 27, 8, 5), '前天 08:05'],
    [at(2026, 9, 26, 8, 5), '9月26日 08:05'], [at(2026, 1, 2, 9, 0), '1月2日 09:00'], [at(2025, 9, 26, 8, 5), '2025年9月26日'],
  ];
  for (const [timestamp, expected] of vectors) assert.equal(relativeTime(timestamp, now), expected, expected);
});

test('相对时间：刚过零点时按日历日而不是 24 小时；未来时间与无效时间不编造文案', () => {
  const midnight = at(2026, 9, 29, 0, 30);
  assert.equal(relativeTime(at(2026, 9, 28, 23, 50), midnight), '40 分钟前');
  assert.equal(relativeTime(at(2026, 9, 28, 22, 0), midnight), '昨天 22:00');
  const now = at(2026, 9, 29, 12, 0);
  assert.equal(relativeTime(now + 3 * 60000, now), '刚刚', '5 分钟内的设备时钟偏差视为刚刚');
  assert.equal(relativeTime(at(2026, 9, 29, 14, 0), now), '9月29日 14:00');
  for (const invalid of [0, -1, NaN]) assert.equal(relativeTime(invalid, now), '');
});

// ---------- 记录文案与语义色调 ----------
const receipt = (itemCount, items, state = 'received') => ({ id: 'r', requestId: 'req', createdAt: 1, itemCount, state,
  progress: items ? { version: 1, items: items.map((status, index) => ({ index, status })) } : null });

test('回执文案只列非零计数；处理中、无进度、已过期各自明确', () => {
  assert.equal(receiptLabel(receipt(2, ['completed', 'completed'])), '成功 2');
  assert.equal(receiptLabel(receipt(3, ['completed', 'failed', 'duplicate'])), '成功 1 · 失败 1 · 重复 1');
  assert.equal(receiptLabel(receipt(2, ['canceled', 'canceled'])), '取消 2');
  assert.equal(receiptLabel(receipt(3, ['completed', 'pending', 'processing'])), '电脑处理中（1/3） · 成功 1');
  assert.equal(receiptLabel(receipt(2, ['pending', 'processing'])), '电脑处理中（0/2）');
  assert.equal(receiptLabel(receipt(1, null, 'accepted')), '中转已接收，等待电脑');
  assert.equal(receiptLabel(receipt(1, null, 'received')), '电脑已接收，等待处理');
  assert.equal(receiptLabel(receipt(1, null, 'expired')), '已过期');
  for (const label of [receiptLabel(receipt(2, ['completed', 'completed'])), receiptLabel(receipt(3, ['completed', 'failed', 'duplicate']))]) {
    assert.doesNotMatch(label, / 0(?!\d)/, '不应出现「成功 0」「重复 0」这类零计数');
  }
});

test('回执色调：全部成功为 success，部分失败 warning，全部失败 error，未完成与无进度为 neutral', () => {
  assert.equal(receiptTone(receipt(2, ['completed', 'duplicate'])), 'success');
  assert.equal(receiptTone(receipt(2, ['completed', 'failed'])), 'warning');
  assert.equal(receiptTone(receipt(2, ['failed', 'failed'])), 'error');
  assert.equal(receiptTone(receipt(2, ['completed', 'processing'])), 'neutral');
  assert.equal(receiptTone(receipt(2, ['canceled', 'canceled'])), 'neutral');
  assert.equal(receiptTone(receipt(1, null, 'accepted')), 'neutral');
  assert.equal(receiptTone(receipt(1, null, 'expired')), 'warning');
});

// ---------- 语义色 ----------
const resourcesDir = path.join(__dirname, '../entry/src/main/resources');
const colors = mode => Object.fromEntries(JSON.parse(fs.readFileSync(path.join(resourcesDir, mode, 'element/color.json'), 'utf8')).color.map(c => [c.name, c.value]));
const luminance = hex => {
  const values = hex.slice(1).match(/../g).map(v => parseInt(v, 16) / 255).map(v => v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4));
  return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722;
};
const contrast = (a, b) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };

test('深浅两套颜色资源的名称完全一致，缺任何一边都会静默回落到另一套', () => {
  assert.deepEqual(Object.keys(colors('dark')).sort(), Object.keys(colors('base')).sort());
});

test('warning / success / error 在所有底色上文字对比度不低于 4.5:1，徽标底色上同样成立', () => {
  for (const mode of ['base', 'dark']) {
    const c = colors(mode);
    for (const name of ['warning', 'success', 'error']) {
      assert.ok(c[name], `${mode}/${name} 缺失`);
      for (const background of ['surface', 'page_background', 'surface_muted']) {
        assert.ok(contrast(c[name], c[background]) >= 4.5, `${mode}/${name} 在 ${background} 上对比度 ${contrast(c[name], c[background]).toFixed(2)}`);
      }
    }
    for (const name of ['warning', 'success']) {
      assert.ok(contrast(c[name], c[`${name}_soft`]) >= 4.5, `${mode}/${name} 在 ${name}_soft 上对比度不足`);
    }
  }
});

// ---------- 版本号 ----------
function version(info, fail = false) {
  const { appVersionLabel } = load('services/AppVersion.ets', { '@kit.AbilityKit': { bundleManager: {
    BundleFlag: { GET_BUNDLE_INFO_DEFAULT: 0 }, getBundleInfoForSelfSync: () => { if (fail) throw new Error('bms'); return info; } } } });
  return appVersionLabel();
}

test('关于页版本号来自安装包；开发包追加标记；读取失败明确显示未知', () => {
  assert.equal(version({ versionName: '0.3.1', name: 'com.couleurapp.guizhi.capture' }), '0.3.1');
  assert.equal(version({ versionName: '0.3.1', name: 'com.couleurapp.guizhi.capture.dev' }), '0.3.1 · 开发包');
  assert.equal(version({ versionName: '', name: 'com.couleurapp.guizhi.capture' }), '版本未知');
  assert.equal(version(null, true), '版本未知');
});

// ---------- 复制 ----------
function copyPage(fail) {
  const copied = [];
  const { ui } = createPage({}, { '@kit.BasicServicesKit': { pasteboard: {
    MIMETYPE_TEXT_PLAIN: 'text/plain', createData: (mime, text) => ({ mime, text }),
    getSystemPasteboard: () => ({ setData: async data => { if (fail) throw new Error('剪贴板不可用'); copied.push(data.text); } }) } } });
  return { ui, copied };
}

test('复制：成功提示由调用方决定；失败给出长按复制的兜底提示，不误报成功', async () => {
  const ok = copyPage(false);
  await ok.ui.copyText('request-id-0001', '请求编号已复制');
  assert.deepEqual(ok.copied, ['request-id-0001']);
  assert.equal(ok.ui.notice, '请求编号已复制');
  const bad = copyPage(true);
  await bad.ui.copyText('request-id-0001', '请求编号已复制');
  assert.equal(bad.ui.notice, '复制失败，请长按文字复制');
  const detail = copyPage(false);
  detail.ui.error = '发送失败'; detail.ui.statusError = '刷新失败'; detail.ui.incomingError = '';
  await detail.ui.copyError();
  assert.deepEqual(detail.copied, ['发送失败\n刷新失败']);
  assert.equal(detail.ui.notice, '错误信息已复制');
});
