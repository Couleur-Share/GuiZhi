#!/usr/bin/env node
/**
 * 在 React 参照里量「悬停上移」在按钮下沿 2px 带内的行为（台账 B-004）：
 * 分段按钮悬停时 `-translate-y-0.5` 上移 2px，命中区随之上移；指针停在原位置下沿内侧 1px 时，
 * 上移后指针落在新位置之外。这里测两件事：
 *   1. 指针不动，悬停状态会不会随上移自己来回切换（逐帧采样 :hover 与 transform）；
 *   2. 指针在该处每动一下，悬停是否随之切换。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-lift-edge.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";
import { getScene } from "../scenes/react-scenes.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = {};
try {
  const { page: win } = session;
  const variant = parseVariantKey("light-royal-blue-medium-1280x800", matrix);
  await applyVariant(session, variant, matrix);
  await win.evaluate(() => {
    [...document.querySelectorAll("style")].filter((s) => s.textContent.includes("transition: none !important")).forEach((s) => s.remove());
    document.documentElement.setAttribute("data-motion", "standard");
  });
  await getScene("settings.appearance").enter({ win, session });
  const seg = win.getByRole("button", { name: "深色", exact: true }).first();
  await seg.scrollIntoViewIfNeeded();
  await win.mouse.move(2, 2);
  await win.waitForTimeout(500);
  const rest = await seg.boundingBox();
  report.rest = rest;
  const x = rest.x + rest.width / 2;
  const y = rest.y + rest.height - 1; // 原位置下沿内侧 1px

  /** 逐帧采样 :hover 与 transform，ms 毫秒。 */
  const sampleHover = (ms) => seg.evaluate(async (el, ms) => {
    const t0 = performance.now();
    const rows = [];
    await new Promise((resolve) => {
      const tick = () => {
        rows.push({ t: Math.round((performance.now() - t0) * 10) / 10, hover: el.matches(":hover"), top: Math.round(el.getBoundingClientRect().top * 100) / 100 });
        if (performance.now() - t0 < ms) requestAnimationFrame(tick); else resolve();
      };
      tick();
    });
    return rows;
  }, ms);

  // 1. 指针移到该处，然后不动，看 600 ms。
  await win.mouse.move(x, y);
  report.stationary = await sampleHover(600);
  const flips = report.stationary.filter((r, i, a) => i > 0 && r.hover !== a[i - 1].hover).length;
  report.stationaryFlips = flips;

  // 2. 指针在该处来回动 0.1px，每次动完等 120 ms 读一次。
  report.moves = [];
  for (let i = 0; i < 8; i++) {
    await win.mouse.move(x, y + (i % 2) * 0.1);
    await win.waitForTimeout(120);
    report.moves.push(await seg.evaluate((el) => ({ hover: el.matches(":hover"), top: Math.round(el.getBoundingClientRect().top * 100) / 100 })));
  }
  const text = JSON.stringify(report, null, 1);
  if (outFile) {
    fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
    fs.writeFileSync(path.resolve(outFile), text + "\n");
    console.log(`已写 ${path.resolve(outFile)}`);
  } else {
    console.log(text);
  }
} finally {
  await session.close();
}
