#!/usr/bin/env node
/**
 * 在 React 参照里量控件的状态样式与过渡（台账 B-004）：
 * 静止 / 悬停 / 键盘聚焦（:focus-visible）/ 按下 四种状态下的计算样式，加上 transition 的属性、时长、曲线，
 * 以及状态变化后若干帧里的取样（量真实的过渡曲线与终点）。
 *
 * 与对等采集器不同：这里**不**注入 SETTLE_CSS（那会把 transition 关掉），数据是真实动画下的读数。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/tools/probe-states.mjs [--out <文件>] [--motion standard|reduced|off]
 *
 * 前提：已有 React 参照构建（node apps/desktop-go/scripts/parity/build-react-ref.mjs）。
 * 输出只含样式读数，不含任何用户数据；会话用一次性 userData（见 react-session.mjs）。
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
const motion = arg("--motion", "standard");

const PROPS = [
  "backgroundColor", "color", "borderTopColor", "borderTopWidth", "boxShadow", "opacity", "transform",
  "transitionProperty", "transitionDuration", "transitionTimingFunction", "transitionDelay",
  "animationName", "animationDuration", "animationTimingFunction", "cursor", "left", "top",
];

/** 读一个元素当前的计算样式与外框。 */
async function readStyle(loc, child = null) {
  return loc.evaluate((el, { props, child }) => {
    const target = child ? el.querySelector(child) : el;
    if (!target) return { missing: child };
    const cs = getComputedStyle(target);
    const r = target.getBoundingClientRect();
    const style = Object.fromEntries(props.map((p) => [p, cs[p]]));
    return { ...style, rect: [r.x, r.y, r.width, r.height].map((v) => Math.round(v * 100) / 100) };
  }, { props: PROPS, child });
}

/** 状态变化之后每帧取样若干属性，返回 [{t, ...}]（t 为毫秒，从调用起算）。 */
async function sample(loc, props, ms, child = null) {
  return loc.evaluate(async (el, { props, ms, child }) => {
    const target = child ? el.querySelector(child) : el;
    const t0 = performance.now();
    const rows = [];
    await new Promise((resolve) => {
      const tick = () => {
        const cs = getComputedStyle(target);
        rows.push({ t: Math.round((performance.now() - t0) * 10) / 10, ...Object.fromEntries(props.map((p) => [p, cs[p]])) });
        if (performance.now() - t0 < ms) requestAnimationFrame(tick); else resolve();
      };
      tick();
    });
    return rows;
  }, { props, ms, child });
}

async function centre(loc) {
  const box = await loc.boundingBox();
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

/** 一个控件的四态读数。pressedAway：按下后把指针挪出控件再松开，免得真的触发点击。 */
async function probeControl(win, name, loc, { child = null, keyboard = true } = {}) {
  const out = { name };
  await loc.scrollIntoViewIfNeeded();
  await win.mouse.move(2, 2);
  await win.waitForTimeout(450);
  out.rest = await readStyle(loc, child);

  const p = await centre(loc);
  await win.mouse.move(p.x, p.y);
  await win.waitForTimeout(450);
  out.hover = await readStyle(loc, child);

  await win.mouse.down();
  await win.waitForTimeout(450);
  out.pressed = await readStyle(loc, child);
  await win.mouse.move(2, 2);
  await win.mouse.up();
  await win.waitForTimeout(450);

  if (keyboard) {
    await win.keyboard.press("Tab"); // 让页面进入键盘模态，之后的程序聚焦才算 :focus-visible
    await loc.evaluate((el) => el.focus());
    await win.waitForTimeout(450);
    out.focusVisible = await readStyle(loc, child);
    await loc.evaluate((el) => el.blur());
    await win.waitForTimeout(100);
  }
  return out;
}

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { motion, controls: [], samples: {} };
try {
  const { page: win } = session;
  const variant = parseVariantKey("light-royal-blue-medium-1280x800", matrix);
  await applyVariant(session, variant, matrix);
  // 去掉采集器的 SETTLE_CSS：这里要看真实的过渡。
  await win.evaluate(() => {
    [...document.querySelectorAll("style")].filter((s) => s.textContent.includes("transition: none !important")).forEach((s) => s.remove());
    document.documentElement.setAttribute("data-motion", "standard");
  });
  await win.evaluate((m) => document.documentElement.setAttribute("data-motion", m), motion);

  // ---- 应用设置页 ----
  await getScene("settings.general").enter({ win, session });
  const row = win.getByText("后台任务", { exact: true }).locator("xpath=ancestor::div[contains(@class,'justify-between')][1]");
  report.controls.push(await probeControl(win, "SettingItem 行（后台任务）", row, { keyboard: false }));
  const sw = win.getByRole("switch", { name: "自动保存", exact: true });
  report.controls.push(await probeControl(win, "开关·关（自动保存或同类）", sw));
  report.controls.push({ name: "开关旋钮（静止）", rest: await readStyle(sw, "span") });
  const select = win.getByRole("button", { name: "关闭窗口行为", exact: true });
  report.controls.push(await probeControl(win, "下拉触发器（关闭窗口行为）", select));

  // 开关的过渡取样：点一下，记录轨道与旋钮每帧的颜色与 left。
  const tracking = ["backgroundColor", "borderTopColor", "boxShadow"];
  const before = await sw.getAttribute("aria-checked");
  const trackSamples = sample(sw, tracking, 320);
  const knobSamples = sample(sw, ["left", "backgroundColor"], 320, "span");
  await sw.click();
  report.samples.switchTrack = await trackSamples;
  report.samples.switchKnob = await knobSamples;
  report.samples.switchBefore = before;
  report.samples.switchAfter = await sw.getAttribute("aria-checked");
  await win.mouse.move(2, 2);
  await sw.click(); // 还原
  await win.waitForTimeout(450);

  // ---- 显示设置页 ----
  await getScene("settings.appearance").enter({ win, session });
  const swatches = win.locator("button.rounded-full.h-10.w-10");
  const custom = win.getByRole("button", { name: "自定义", exact: true }).first();

  // 自定义面板的入场：先确保面板是收起的（点第一枚预设色板），再点「自定义」，逐帧取面板的 opacity 与 transform。
  await swatches.first().click();
  await win.mouse.move(2, 2);
  await win.waitForTimeout(500);
  const hexInput = win.getByPlaceholder("#3b82f6", { exact: true });
  report.samples.panelClosedBefore = (await hexInput.count()) === 0;
  const panelSamples = win.evaluate(async () => {
    const t0 = performance.now();
    const rows = [];
    await new Promise((resolve) => {
      const tick = () => {
        const input = document.querySelector('input[placeholder="#3b82f6"]');
        const panel = input?.closest('[class*="slide-in-from-bottom-2"]');
        if (panel) {
          const cs = getComputedStyle(panel);
          const r = panel.getBoundingClientRect();
          rows.push({ t: Math.round((performance.now() - t0) * 10) / 10, opacity: cs.opacity, transform: cs.transform, top: r.top, animationName: cs.animationName, animationDuration: cs.animationDuration, animationTimingFunction: cs.animationTimingFunction, animationFillMode: cs.animationFillMode });
        }
        if (performance.now() - t0 < 500) requestAnimationFrame(tick); else resolve();
      };
      tick();
    });
    return rows;
  });  await custom.click();
  report.samples.customPanel = await panelSamples;
  await win.mouse.move(2, 2);
  await win.waitForTimeout(500);

  // 分段按钮：主题模式（浅色选中，深色未选中）。
  const unselected = win.getByRole("button", { name: "深色", exact: true }).first();
  const selected = win.getByRole("button", { name: "浅色", exact: true }).first();
  report.controls.push(await probeControl(win, "分段按钮·未选中（深色）", unselected));
  report.controls.push(await probeControl(win, "分段按钮·选中（浅色）", selected));
  report.controls.push(await probeControl(win, "色板（自定义，选中）", custom));
  report.controls.push(await probeControl(win, "色板（第一枚预设，未选中）", swatches.first()));

  // 悬停上移的过渡取样：未选中与选中各一次。
  for (const [key, seg] of [["segmentLiftUnselected", unselected], ["segmentLiftSelected", selected]]) {
    await seg.scrollIntoViewIfNeeded();
    await win.mouse.move(2, 2);
    await win.waitForTimeout(500);
    const c = await centre(seg);
    const rows = sample(seg, ["transform", "boxShadow"], 320);
    await win.mouse.move(c.x, c.y);
    report.samples[key] = await rows;
    await win.mouse.move(2, 2);
    await win.waitForTimeout(500);
  }

  // 选中状态切换的取样：点「深色」前后各取轨迹会改主题，这里改取「字号」分段（第二组）：点一个未选中的字号，记录它与旧选中项的变化。
  const fontSegments = win.getByRole("button", { name: /^(小|中|大)$/ });
  report.samples.fontSegmentCount = await fontSegments.count();
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
