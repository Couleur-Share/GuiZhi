const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { pathToFileURL } = require('node:url');

const script = path.join(__dirname, '../scripts/layout-find.mjs');
let lib;
test.before(async () => { lib = await import(pathToFileURL(script).href); });

const node = (bounds, attributes = {}, children = []) => ({ attributes: { bounds, ...attributes }, children });
const layout = node('[0,0][1000,2000]', { type: 'root' }, [
  node('[100,100][300,200]', { text: '新建', type: 'Text' }),
  node('[100,300][500,400]', { text: '新建记录', type: 'Text' }),
  node('[100,500][300,600]', { text: '新建', type: 'Text', id: 'primary' }),
  node('[100,2100][300,2200]', { text: '新建', type: 'Text' }), // 屏幕外
  node('[0,0][0,0]', { text: '零尺寸', type: 'Text' }),
  node('bad', { text: '坏边界', type: 'Text' }),
  node('[10,10][30,30]', { description: '返回', key: 'back_button', type: 'Image' }),
]);

test('bounds 只接受有正面积的合法格式，中心点四舍五入', () => {
  assert.deepEqual({ ...lib.parseBounds('[0,0][1260,2720]') }, { left: 0, top: 0, right: 1260, bottom: 2720, x: 630, y: 1360 });
  assert.equal(lib.parseBounds('[1,1][2,2]').x, 2);
  for (const bad of ['', 'x', '[0,0][0,10]', '[5,5][1,1]', undefined, null]) assert.equal(lib.parseBounds(bad), undefined);
});

test('精确匹配优先于包含匹配，屏幕外的排在屏幕内之后', () => {
  const hits = lib.findNodes(layout, '新建');
  assert.deepEqual(hits.map(hit => [hit.x, hit.y, hit.onScreen]), [[200, 150, true], [200, 550, true], [200, 2150, false], [300, 350, true]]);
});

test('可按描述、id/key 查找，零尺寸与坏边界的控件被忽略', () => {
  assert.deepEqual(lib.findNodes(layout, '返回').map(hit => [hit.x, hit.y]), [[20, 20]]);
  assert.deepEqual(lib.findNodes(layout, 'back_button', 'id').map(hit => hit.type), ['Image']);
  assert.deepEqual(lib.findNodes(layout, 'primary', 'id').map(hit => hit.text), ['新建']);
  assert.equal(lib.findNodes(layout, '零尺寸').length, 0);
  assert.equal(lib.findNodes(layout, '坏边界').length, 0);
  assert.equal(lib.findNodes(layout, '').length, 0);
  // id 模式不做包含匹配，避免误点相近控件。
  assert.equal(lib.findNodes(layout, 'back', 'id').length, 0);
});

test('界面文字按控件树顺序去重列出', () => {
  assert.deepEqual(lib.listTexts(layout), ['新建', '新建记录', '返回']);
});

test('命令行输出中心点；找不到时退出码 3；参数错误退出码 2', () => {
  const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-layout-')), 'layout.json');
  fs.writeFileSync(file, JSON.stringify(layout));
  const run = (...args) => spawnSync(process.execPath, [script, ...args], { encoding: 'utf8' });
  assert.equal(run(file, '新建记录').stdout.trim(), '300 350');
  assert.equal(run(file, '新建', '--index', '1').stdout.trim(), '200 550');
  assert.equal(run(file, '不存在').status, 3);
  assert.equal(run(file, '新建', '--index', '9').status, 3);
  assert.equal(run(file).status, 2);
  assert.match(run('--texts', file).stdout, /新建记录/);
});
