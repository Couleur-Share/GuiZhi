#!/usr/bin/env node
/**
 * 在 React 参照里量 Chromium 的「定高 flex 列」如何收缩子项（台账 B-034）：
 * 知识库侧栏外层是 `flex h-full min-h-0 flex-col overflow-y-auto`，行（LibrarySidebarRow）是 `min-h-[32px]` 的弹性项，
 * 内容高随字号档变（大字号 36px、中 32px、小 28px）。内容总高超出容器时，行按 flex-shrink 收缩，最低收到显式的 32px；
 * 分区标题与说明文字的 min-height:auto 等于内容高，不收缩。这里把这种列搭在页面上，读出每个子项的上缘与高度，
 * 作为 kit.FlexColumn 的 L4 用例期望值（Chromium 的运算是 1/64 px 的定点数，舍入规则不照抄会差出零点几像素）。
 *
 * 搭的子项（容器是 display:flex; flex-direction:column; height:room; overflow-y:auto）：
 *   fix  外层 div 无显式高度，里面一个定高的孩子（min-height:auto 等于内容高，不收缩），可带上、下外边距与 margin-top:auto；
 *   row  外层 `display:flex; align-items:center; min-height:32px`，里面一个 content 高的孩子（行：可收缩到 32）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-flex-shrink.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { openReactSession } from "../lib/react-session.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);

const MIN = 32; // min-h-[32px]：px 字面量
// 行的内容高：大字号 36、中 32、小 28，另加两个带小数的（字号档以外的字号 / 缩放）。
const CONTENTS = [36, 32, 28, 40.5, 33.75];
// 分区的版式：各组行数；组与组之间是「标题」（h 18、上 18、下 4.5），首部是范围页签，尾部是 mt-auto 的回收站块。
const SHAPES = [[3], [1, 1], [4, 3], [7, 3, 5], [12, 4, 6, 2], [25, 3, 9, 1], [40]];
const HEADING = { t: "fix", h: 18, mt: 18, mb: 4.5 };
const HINT = { t: "fix", h: 27, mt: 0, mb: 0 };
const TABS = { t: "fix", h: 67.5, mt: 0, mb: 0 };
const TRASH = { t: "fix", h: 54, mt: 0, mb: 0, auto: true };

const cases = [];
for (const content of CONTENTS) {
  for (const shape of SHAPES) {
    const items = [TABS];
    shape.forEach((rows, g) => {
      items.push(HEADING);
      if (g === 1 && rows === 1) items.push(HINT);
      for (let i = 0; i < rows; i += 1) items.push({ t: "row", content, min: MIN });
    });
    items.push(TRASH);
    const rowCount = shape.reduce((a, b) => a + b, 0);
    const natural = items.reduce((s, it) => s + (it.t === "row" ? Math.max(it.content, it.min) : it.mt + it.h + it.mb), 0);
    const spans = [30, 1, 0.5, 0, -0.5, -1, -3, -7.3, -13.37, -2.4 * rowCount, -4 * rowCount + 1, -4 * rowCount, -4 * rowCount - 1, -4 * rowCount - 30, -natural * 0.5];
    for (const d of spans) {
      const room = Math.max(natural + d, 0);
      cases.push({ room: Math.round(room * 64) / 64, items });
    }
  }
}

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const { page: win } = session;
  const result = await win.evaluate(({ cs }) => {
    const host = document.createElement("div");
    host.style.cssText = "position:absolute;left:0;top:0;visibility:hidden;pointer-events:none;width:400px;";
    document.body.appendChild(host);
    const px = (v) => `${v}px`;
    const out = [];
    for (const c of cs) {
      const box = document.createElement("div");
      box.style.cssText = `display:flex;flex-direction:column;height:${px(c.room)};overflow-y:auto;width:300px;`;
      host.appendChild(box);
      const els = c.items.map((it) => {
        const outer = document.createElement("div");
        const inner = document.createElement("div");
        if (it.t === "row") {
          outer.style.cssText = `display:flex;align-items:center;min-height:${px(it.min)};`;
          inner.style.cssText = `height:${px(it.content)};width:10px;flex:none;`;
        } else {
          outer.style.cssText = `margin-top:${it.auto ? "auto" : px(it.mt)};margin-bottom:${px(it.mb)};`;
          inner.style.cssText = `height:${px(it.h)};`;
        }
        outer.appendChild(inner);
        box.appendChild(outer);
        return outer;
      });
      const top = box.getBoundingClientRect().y;
      out.push(els.map((el) => {
        const r = el.getBoundingClientRect();
        return [r.y - top, r.height];
      }));
      box.remove();
    }
    host.remove();
    return out;
  }, { cs: cases });

  // 换成 1/64 像素的整数（LayoutUnit）；不是整数倍的读数说明测量有问题，直接报错
  const U = 64;
  const toU = (v) => {
    const x = v * U;
    if (Math.abs(x - Math.round(x)) > 1e-6) throw new Error(`读数 ${v} 不是 1/64 的整数倍`);
    return Math.round(x);
  };
  const report = {
    note: "Chromium 定高 flex 列收缩子项的实测，单位 1/64 px（LayoutUnit）；台账 B-034 的 kit.FlexColumn L4 用例的期望值，由 probe-flex-shrink.mjs 量出",
    chromium: await win.evaluate(() => navigator.userAgent),
    unit: U,
    // 子项：["fix", 内容高, 上外边距, 下外边距, margin-top:auto 记 1] 或 ["row", 内容高, 最小高]
    // 用例：[容器高, [子项...], [[上缘, 高]...]]
    cases: cases.map((c, i) => [
      toU(c.room),
      c.items.map((it) => (it.t === "row" ? ["row", toU(it.content), toU(it.min)] : ["fix", toU(it.h), toU(it.mt), toU(it.mb), it.auto ? 1 : 0])),
      result[i].map(([y, h]) => [toU(y), toU(h)]),
    ]),
  };
  const text = JSON.stringify(report);
  if (outFile) {
    fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
    fs.writeFileSync(outFile, text);
  }
  console.log(`用例 ${report.cases.length}；${(text.length / 1024).toFixed(0)} KiB`);
} finally {
  await session.close();
}
