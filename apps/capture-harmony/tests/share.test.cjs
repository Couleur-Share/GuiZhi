const test = require('node:test');
const assert = require('node:assert/strict');
const { load } = require('./support.cjs');

const shareRecords = load('core/ShareRecords.ts');
const { ShareRecordView, pickShareText, shareSavedNotice, shareFailureMessage } = shareRecords;
const view = (utd, content = '') => new ShareRecordView(utd, content);
const link = 'https://example.com/article?utm_source=keep&x=2';

test('分享记录选择：链接、文字、多条按原样换行拼接，不改写 URL 参数', () => {
  assert.equal(pickShareText([view('general.hyperlink', link)]).text, link);
  assert.equal(pickShareText([view('general.plain-text', '一句想法')]).text, '一句想法');
  const both = pickShareText([view('general.plain-text', '文章标题'), view('general.hyperlink', link), view('general.text', ' 尾注 ')]);
  assert.equal(both.text, `文章标题\n${link}\n 尾注 `);
  assert.equal(both.ignored, 0);
});

test('混合分享保留文字与链接，忽略图片和文件并计数', () => {
  const mixed = pickShareText([view('general.image', 'file://media/1'), view('general.hyperlink', link), view('general.file'), view('general.video')]);
  assert.equal(mixed.text, link);
  assert.equal(mixed.ignored, 3);
});

test('空白文字记录被略过；没有任何可用内容时才失败，并区分原因', () => {
  assert.equal(pickShareText([view('general.text', '  \n'), view('general.hyperlink', link)]).text, link);
  assert.throws(() => pickShareText([]), /分享内容为空/);
  assert.throws(() => pickShareText([view('general.text', '   ')]), /分享缺少文字内容/);
  assert.throws(() => pickShareText([view('general.image'), view('general.file')]), /暂不支持图片或文件分享/);
  assert.throws(() => pickShareText([view('general.image'), view('general.text', ' ')]), /暂不支持图片或文件分享/);
});

test('保存提示按忽略数量变化；失败提示带具体原因，缺失时退回通用说明', () => {
  assert.equal(shareSavedNotice(0), '分享已保存到本机草稿，确认后发送');
  assert.match(shareSavedNotice(2), /已忽略 2 项图片或文件/);
  assert.equal(shareFailureMessage(new Error('本机草稿已满（100 条），请先发送或清理后再分享')),
    '本次分享未保存：本机草稿已满（100 条），请先发送或清理后再分享');
  assert.match(shareFailureMessage(new Error('')), /本次分享未保存。请保留原内容/);
});

// 入口 Ability：只替换系统边界，选择、保存与提示走真实代码。
function ability({ records, open, receive } = {}) {
  const storage = new Map(), received = [];
  const AppStorage = { setOrCreate: (key, value) => storage.set(key, value), get: key => storage.get(key) };
  const { default: EntryAbility } = load('entryability/EntryAbility.ets', {
    '@kit.AbilityKit': { UIAbility: class {}, AbilityConstant: {} }, '@kit.ArkUI': { window: {} },
    '@kit.ShareKit': { systemShare: { getSharedData: async () => ({ getRecords: () => records }) } },
    '@kit.ArkTS': { util: { generateRandomUUID: () => 'share-uuid-0000000001' } },
    '../services/CaptureRuntime': { CaptureRuntime: { open: async () => {
      if (open) throw open;
      return { queue: { receive: async (id, text) => { if (receive) throw receive; received.push({ id, text }); } } };
    } } },
    '../services/WindowAppearance': { WindowAppearance: class {} }, '../core/ShareRecords': shareRecords,
  }, { AppStorage });
  const instance = new EntryAbility();
  instance.context = {};
  const share = async () => { instance.receive({ action: 'ohos.want.action.sendData' }); await new Promise(resolve => setImmediate(resolve)); };
  return { storage, received, share, instance };
}

test('入口分享：纯链接保存并提示；修订号递增让页面重新读取草稿', async () => {
  const { storage, received, share } = ability({ records: [{ utd: 'general.hyperlink', content: link }] });
  await share();
  assert.deepEqual(received.map(item => item.text), [link]);
  assert.equal(storage.get('captureNotice'), '分享已保存到本机草稿，确认后发送');
  assert.equal(storage.get('captureError'), '');
  assert.equal(storage.get('captureRevision'), 1);
});

test('入口分享：带图片的分享保留文字并提示已忽略数量，不再整体拒绝', async () => {
  const { storage, received, share } = ability({ records: [{ utd: 'general.image', content: 'x' }, { utd: 'general.plain-text', content: '好文章' }] });
  await share();
  assert.deepEqual(received.map(item => item.text), ['好文章']);
  assert.match(storage.get('captureNotice'), /已忽略 1 项图片或文件/);
  assert.equal(storage.get('captureError'), '');
});

test('入口分享失败：给出具体原因且不产生保存提示（仅图片、草稿已满、存储不可用）', async () => {
  const cases = [
    [{ records: [{ utd: 'general.image', content: 'x' }] }, /本次分享未保存：暂不支持图片或文件分享/],
    [{ records: [{ utd: 'general.text', content: '文字' }], receive: new Error('本机草稿已满（100 条），请先发送或清理后再分享') }, /本次分享未保存：本机草稿已满/],
    [{ records: [{ utd: 'general.text', content: '文字' }], open: new Error('安全存储不可用') }, /本次分享未保存：安全存储不可用/],
  ];
  for (const [options, expected] of cases) {
    const { storage, received, share } = ability(options);
    await share();
    assert.match(storage.get('captureError'), expected);
    assert.equal(storage.get('captureNotice'), undefined);
    assert.equal(received.length, 0);
  }
});

test('入口忽略非分享意图，不读取分享数据也不保存', async () => {
  const { storage, received, instance } = ability({ records: [{ utd: 'general.text', content: '文字' }] });
  instance.receive({ action: 'action.system.home' });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(received.length, 0);
  assert.equal(storage.size, 0);
});
