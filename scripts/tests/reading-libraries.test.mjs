import assert from "node:assert/strict";
import test from "node:test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { readingLibraryModule, readingLibrariesPlugin } from "../../apps/desktop/scripts/reading-libraries-plugin.mjs";
import { createServer, build } from "../../apps/desktop/node_modules/vite/dist/node/index.js";

test("阅读库按需从产物相邻资源读取，保留原文且不缓存大字符串", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-reading-assets-"));
  try {
    const assets = [];
    const value = {
      compiler: "编译器\u2028</script>",
      animation: "动画",
      components: { card: ".card{}" },
      runtime: { echarts: "图表", mermaid: "流程图", animation: "交互动画" },
    };
    const source = readingLibraryModule(value, (asset) => assets.push(asset));
    let reads = 0;
    const context = {
      fs: {
        readFileSync(...args) {
          reads++;
          return fs.readFileSync(...args);
        },
      },
      path,
      __dirname: root,
    };
    // 执行生成模块的主体，资源尚不存在：启动阶段不能读取资源。
    vm.runInNewContext(
      source
        .replace(/import .*?;import .*?;/, "")
        .replace("export default ", "result = "),
      context,
    );
    assert.equal(reads, 0);
    assert.throws(() => context.result.compiler, /阅读组件资源读取失败/);
    for (const asset of assets) {
      const file = path.join(root, asset.fileName);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, asset.source);
    }
    assert.equal(context.result.compiler, value.compiler);
    assert.equal(context.result.animation, value.animation);
    for (const name of Object.keys(value.runtime))
      assert.equal(context.result.runtime[name], value.runtime[name]);
    assert.equal(context.result.components.card, value.components.card);
    fs.writeFileSync(
      path.join(root, "reading-libraries/compiler.js"),
      "已替换",
    );
    assert.equal(context.result.compiler, "已替换");
    assert.equal(assets.length, 5);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("真实 Vite serve 可读取全部阅读资源，关闭实例后释放临时文件", async () => {
  const value = { compiler: '编译器', animation: '动画', components: { card: '.card{}' },
    runtime: { echarts: '图表', mermaid: '流程图', animation: '交互' } };
  const server = await createServer({ configFile: false, server: { middlewareMode: true },
    plugins: [readingLibrariesPlugin({ buildLibraries: async () => value })] });
  let libraries;
  try {
    libraries = (await server.ssrLoadModule('virtual:reading-libraries')).default;
    assert.equal(libraries.compiler, value.compiler);
    assert.equal(libraries.animation, value.animation);
    for (const key of Object.keys(value.runtime)) assert.equal(libraries.runtime[key], value.runtime[key]);
    assert.deepEqual(libraries.components, value.components);
  } finally {
    await server.close();
  }
  assert.throws(() => libraries.animation, /阅读组件资源读取失败/);
});

test("真实生产构建包含五份相邻资源，运行代码不含测试临时路径", async () => {
  const value = { compiler: '编译器', animation: '动画', components: {},
    runtime: { echarts: '图表', mermaid: '流程图', animation: '交互' } };
  const result = await build({ configFile: false, logLevel: 'silent',
    plugins: [readingLibrariesPlugin({ buildLibraries: async () => value })],
    build: { write: false, lib: { entry: 'virtual:reading-libraries', formats: ['cjs'] },
      rollupOptions: { input: 'virtual:reading-libraries', external: ['node:fs', 'node:path'] } } });
  const output = (Array.isArray(result) ? result[0] : result).output;
  const assets = output.filter(item => item.type === 'asset');
  assert.equal(assets.length, 5);
  assert.equal(assets.find(item => item.fileName === 'reading-libraries/animation.js').source, value.animation);
  const code = output.find(item => item.type === 'chunk').code;
  assert.match(code, /__dirname/);
  assert.doesNotMatch(code, /guizhi-reading-serve-/);
});
