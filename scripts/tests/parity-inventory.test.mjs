/**
 * Viva 对等工程盘点清单的守卫：
 *   - 分片里的场景/对话框/交互 ID 唯一、模块合法、状态行有所属场景根；
 *   - docs/agents/viva-parity-inventory.md 里由脚本生成的三个分母区块，与按当前分片和渲染层源码重新统计的结果一致
 *     （渲染层或分片变了而没刷新分母，这里会红：运行各脚本的 --write 即可）；
 *   - 文案：zh/en 键集合一致，渲染层引用的键都存在。
 */
import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import { check, loadParts, renderMarkdown as statsMarkdown } from "../../apps/desktop-go/scripts/parity/inventory-stats.mjs";
import { renderMarkdown as i18nMarkdown, scan } from "../../apps/desktop-go/scripts/parity/inventory-i18n.mjs";
import { indexTests, renderMarkdown as testsMarkdown } from "../../apps/desktop-go/scripts/parity/inventory-tests.mjs";
import { INDEX_DOC, readBlock, replaceBlock } from "../../apps/desktop-go/scripts/parity/lib/doc-block.mjs";

const doc = fs.readFileSync(INDEX_DOC, "utf8");

test("盘点分片：ID 唯一、模块合法、状态行都有所属场景根或对话框", () => {
  const parts = loadParts();
  assert.ok(parts.length >= 9, `分片数量不足：${parts.length}`);
  assert.deepEqual(check(parts), []);
});

test("盘点分母：场景/对话框/交互区块与分片一致", () => {
  assert.equal(readBlock(doc, "stats"), statsMarkdown(loadParts()).trim(), "stats 区块过期，运行 inventory-stats.mjs --write");
});

test("盘点分母：文案区块与渲染层源码一致，zh/en 键集合一致且无缺失键", () => {
  const r = scan();
  assert.deepEqual([...r.zhNotEn], [], "zh 有而 en 无的键");
  assert.deepEqual([...r.enNotZh], [], "en 有而 zh 无的键");
  assert.deepEqual([...r.rawKeyLeaks], [], "源码引用了既不在 zh.json 又没有默认文案的键");
  assert.equal(readBlock(doc, "i18n"), i18nMarkdown(r).trim(), "i18n 区块过期，运行 inventory-i18n.mjs --write");
});

test("盘点分母：vitest 用例区块与测试源码一致", () => {
  assert.equal(readBlock(doc, "tests"), testsMarkdown(indexTests()).trim(), "tests 区块过期，运行 inventory-tests.mjs --write");
});

test("文档区块工具：CRLF 输入可往返，缺少标记会报错", () => {
  const text = "头\r\n<!-- parity:x:begin -->\r\n旧\r\n<!-- parity:x:end -->\r\n尾\r\n";
  assert.equal(readBlock(text, "x"), "旧");
  const next = replaceBlock(text, "x", "新\n行");
  assert.equal(readBlock(next, "x"), "新\n行");
  assert.ok(!next.includes("\r"), "替换结果应统一为 LF");
  assert.throws(() => readBlock("没有标记", "x"), /缺少区块标记/);
});
