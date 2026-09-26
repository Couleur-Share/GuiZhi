import assert from "node:assert/strict";
import test from "node:test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { readingLibraryModule } from "../../apps/desktop/scripts/reading-libraries-plugin.mjs";

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
