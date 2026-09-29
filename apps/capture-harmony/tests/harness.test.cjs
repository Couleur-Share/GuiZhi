const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { BOUNDARY, pageSource, root } = require('./support.cjs');

const indexSource = fs.readFileSync(path.join(root, 'pages/Index.ets'), 'utf8');

test('Index.ets 恰好一个 test-boundary，标记之前没有 Builder，之后仍有 Builder', () => {
  const parts = indexSource.split(BOUNDARY);
  assert.equal(parts.length, 2, '标记数量必须为 1');
  assert.doesNotMatch(parts[0], /@Builder/, '控制逻辑区不能出现 Builder，否则会被 VM 装载器执行');
  assert.match(parts[1], /@Builder/, '标记不应放在文件末尾');
});

test('标记缺失或重复时装载器直接失败，不会静默截断整页控制逻辑', () => {
  assert.throws(() => pageSource('struct Index {}'), /恰好包含一个/);
  assert.throws(() => pageSource(`struct Index {}\n${BOUNDARY}\n${BOUNDARY}`), /恰好包含一个/);
});

test('装饰器擦除只处理已知装饰器，@Local 不会误吃 @LocalStorageLink', () => {
  const source = pageSource(`struct Index {\n  @LocalStorageLink('a') @Watch('w') value: number = 1;\n  @Local other: number = 2;\n  ${BOUNDARY}\n}`);
  assert.doesNotMatch(source, /@/);
  assert.match(source, /value: number = 1/);
  assert.match(source, /other: number = 2/);
});
