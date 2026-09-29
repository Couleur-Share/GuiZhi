import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

// 鸿蒙端编辑面板的纯逻辑：粘贴合并规则。
// 鸿蒙工程不属于 npm 工作区；通过 tsx 的 CJS 钩子执行同一份客户端逻辑。
const require = createRequire(import.meta.url);
const { pasteInto, EDITOR_MAX_CHARS }: typeof import('../../capture-harmony/entry/src/main/ets/core/EditorPaste') =
  require('../../capture-harmony/entry/src/main/ets/core/EditorPaste.ts');

test('粘贴到空输入框：直接采用剪贴板内容，去掉尾随空白', () => {
  const outcome = pasteInto('', 'https://example.com/a\r\n\n');
  assert.deepEqual([outcome.text, outcome.changed, outcome.message], ['https://example.com/a', true, '']);
});

test('粘贴到只有空白的输入框：替换空白，开头不留空行', () => {
  const outcome = pasteInto('  \n ', '  缩进保留的文字');
  assert.equal(outcome.text, '  缩进保留的文字');
  assert.equal(outcome.changed, true);
});

test('粘贴到已有内容之后：以换行分隔；已以换行结尾则不重复', () => {
  assert.equal(pasteInto('第一条', 'https://example.com/b').text, '第一条\nhttps://example.com/b');
  assert.equal(pasteInto('第一条\n', 'https://example.com/b').text, '第一条\nhttps://example.com/b');
  assert.equal(pasteInto('第一条\n\n', 'x').text, '第一条\n\nx', '不吞用户自己留的空行');
});

test('剪贴板为空或只有空白：不改动输入并给出原因', () => {
  for (const clipboard of ['', '   \n\t ', '\u3000']) {
    const outcome = pasteInto('已有内容', clipboard);
    assert.deepEqual([outcome.text, outcome.changed, outcome.message], ['已有内容', false, '剪贴板里没有文字'], JSON.stringify(clipboard));
  }
});

test('超过输入上限：整体拒绝、不截断、原内容不变；恰好达到上限可以粘贴', () => {
  const existing = 'a'.repeat(EDITOR_MAX_CHARS - 5);
  const refused = pasteInto(existing, 'b'.repeat(10));
  assert.equal(refused.changed, false);
  assert.equal(refused.text, existing, '不做部分粘贴');
  assert.match(refused.message, /超过 32768 字上限，未粘贴/);
  // existing + '\n' + 4 个字符 = 上限
  const exact = pasteInto(existing, 'b'.repeat(4));
  assert.equal(exact.changed, true);
  assert.equal(exact.text.length, EDITOR_MAX_CHARS);
  assert.equal(pasteInto(existing, 'b'.repeat(5)).changed, false, '多一个字符就拒绝');
  // 尾随空白不计入：超长但去掉尾随空白后恰好可放下的内容可以粘贴
  assert.equal(pasteInto('', 'c'.repeat(EDITOR_MAX_CHARS) + '\n\n\n').changed, true);
});

test('超长的纯空白剪贴板也能快速判空（不因正则回溯卡住）', () => {
  const started = Date.now();
  const outcome = pasteInto('', ' '.repeat(2_000_000) + 'x' + ' '.repeat(2_000_000));
  assert.equal(outcome.changed, false, '内容过长被拒绝');
  assert.ok(Date.now() - started < 1000, '处理时间应为线性');
  assert.equal(pasteInto('', ' '.repeat(2_000_000)).message, '剪贴板里没有文字');
});
