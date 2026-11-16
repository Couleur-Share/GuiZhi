/**
 * Viva 对等工程展开矩阵与运行档位的守卫（docs/agents/viva-ui-development.md §2.1、§2.6）：
 *   - 色相预设与字号档读自渲染层源码，当前必须是 6 个色相 × 3 档字号 × 3 个视口 × 2 种明暗；
 *   - 档位大小固定为 T0=2、T1=4、T2=36、T3=108，变体键唯一且能往返解析；
 *   - 渲染层增删色相/字号档时这里会红：先按 §2 的规矩评估矩阵与档位，再更新本测试与文档。
 */
import assert from "node:assert/strict";
import test from "node:test";
import { loadMatrix, parseAppearanceSource, parseVariantKey, tierVariants, variantKey } from "../../apps/desktop-go/scripts/parity/lib/matrix.mjs";

const matrix = loadMatrix();

test("展开矩阵：6 个色相预设、3 档字号、3 个视口、2 种明暗", () => {
  assert.deepEqual(matrix.hues.map((h) => h.id), ["royal-blue", "blue", "purple", "green", "orange", "teal"]);
  assert.deepEqual(matrix.fonts.map((f) => [f.id, f.px]), [["small", 14], ["medium", 16], ["large", 18]]);
  assert.deepEqual(matrix.viewports.map((v) => v.id), ["1280x800", "800x600", "1920x1080"]);
  assert.deepEqual(matrix.modes, ["light", "dark"]);
});

test("运行档位：T0=2、T1=4、T2=36、T3=108，T3 是完整矩阵", () => {
  assert.deepEqual(["T0", "T1", "T2", "T3"].map((t) => tierVariants(t, matrix).length), [2, 4, 36, 108]);
  assert.equal(matrix.modes.length * matrix.hues.length * matrix.fonts.length * matrix.viewports.length, 108);
});

test("变体键：唯一且可往返解析", () => {
  const all = tierVariants("T3", matrix);
  const keys = all.map(variantKey);
  assert.equal(new Set(keys).size, all.length);
  for (const v of all) assert.deepEqual(parseVariantKey(variantKey(v), matrix), v);
  assert.throws(() => parseVariantKey("light-nope-medium-1280x800", matrix), /无法解析/);
});

test("T0/T1 是 T3 的子集，T1 包含 T0", () => {
  const t3 = new Set(tierVariants("T3", matrix).map(variantKey));
  const t0 = tierVariants("T0", matrix).map(variantKey);
  const t1 = new Set(tierVariants("T1", matrix).map(variantKey));
  for (const k of t1) assert.ok(t3.has(k), `${k} 不在 T3 里`);
  for (const k of t0) assert.ok(t1.has(k), `${k} 不在 T1 里`);
});

test("读不到预设时明确报错，不静默得到空矩阵", () => {
  assert.throws(() => parseAppearanceSource("export const nothing = 1;"), /MORANDI_THEMES/);
});
