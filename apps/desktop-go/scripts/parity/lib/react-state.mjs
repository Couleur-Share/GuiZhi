/**
 * 把 React 参照置于一个确定的外观与视口：写 localStorage → reload → 钉死视口（设备像素比 1）→ 压掉动画与光标。
 *
 * 为什么用 Emulation 钉视口：Go 壳在 -bench 下窗口尺寸固定，且本机显示缩放可能是 200%（devicePixelRatio=2），
 * 对等比较必须在「CSS 像素 = 设备像素」的口径下做。Emulation.setDeviceMetricsOverride 钉视口尺寸；
 * 设备缩放本身由 react-session.mjs 的 --force-device-scale-factor=1 钉死（单靠 Emulation 时排版取整仍按宿主 2×）；
 * 截图与测量都走 CDP 直连（Page.captureScreenshot、页内 getBoundingClientRect），不用 Playwright 的 page.screenshot
 * （它会按真实 DPR 放大到 2×）也不用 DOMSnapshot（它的坐标是真实设备像素）。实测记录见 viva-ui-development.md §8。
 */

/** 与 pnpm shot 的默认截图选项同义：关动画、藏光标，否则会抓到淡入过渡的中间帧。 */
export const SETTLE_CSS = `
*, *::before, *::after {
  animation: none !important;
  transition: none !important;
  caret-color: transparent !important;
  scroll-behavior: auto !important;
}
`;

const SETTINGS_KEY = "guizhi-settings";
const SETTINGS_VERSION = 3; // stores/settings.store.ts 的 persist version

/**
 * 写入外观设置与「已处理」标记，让首次引导与迁移提示不出现。
 * 色相预设要把 themeColor、themeHue、themeSaturation 三个字段一起写：启动时按后两者设置 --theme-hue / --theme-saturation，
 * 只写 themeColor 会悄悄回落到默认色相（royal-blue 恰好是默认值，所以只测它时发现不了）。
 */
async function writeLocalState(page, variant, hue) {
  await page.evaluate(({ key, version, variant: v, hue: h }) => {
    const stored = JSON.parse(localStorage.getItem(key) || "null") ?? { state: {}, version };
    stored.version = version;
    stored.state = {
      ...stored.state,
      language: "zh",
      themeMode: v.mode,
      isDarkMode: v.mode === "dark",
      themeColor: v.hue,
      themeHue: h.hue,
      themeSaturation: h.saturation,
      fontSize: v.font,
    };
    localStorage.setItem(key, JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  }, { key: SETTINGS_KEY, version: SETTINGS_VERSION, variant, hue });
}

/** 读回页面上实际生效的外观，用于确认变体确实应用了（而不是悄悄回落到默认）。 */
export async function readAppliedAppearance(page) {
  return page.evaluate(() => {
    const root = document.documentElement;
    const cs = getComputedStyle(root);
    return {
      dark: root.classList.contains("dark"),
      rootFontPx: parseFloat(cs.fontSize),
      hue: cs.getPropertyValue("--theme-hue").trim(),
      saturation: cs.getPropertyValue("--theme-saturation").trim(),
      lang: root.lang,
      motion: root.getAttribute("data-motion"),
      dpr: window.devicePixelRatio,
      viewport: [innerWidth, innerHeight],
    };
  });
}

/**
 * 应用一个变体。matrix 来自 lib/matrix.mjs 的 loadMatrix()。
 * 返回读回的外观（并在与期望不符时抛错）。
 */
export async function applyVariant(session, variant, matrix) {
  const { page, cdp } = session;
  const viewport = matrix.viewports.find((v) => v.id === variant.viewport);
  const font = matrix.fonts.find((f) => f.id === variant.font);
  const hue = matrix.hues.find((h) => h.id === variant.hue);
  if (!viewport || !font || !hue) throw new Error(`变体取值不在矩阵内：${JSON.stringify(variant)}`);

  await writeLocalState(page, variant, hue);
  await session.reload();
  await cdp.send("Emulation.setDeviceMetricsOverride", { width: viewport.width, height: viewport.height, deviceScaleFactor: 1, mobile: false });
  await page.addStyleTag({ content: SETTLE_CSS });
  await settle(page);

  const applied = await readAppliedAppearance(page);
  const problems = [];
  if (applied.dark !== (variant.mode === "dark")) problems.push(`dark=${applied.dark}，期望 ${variant.mode === "dark"}`);
  if (applied.rootFontPx !== font.px) problems.push(`根字号 ${applied.rootFontPx}px，期望 ${font.px}px`);
  if (Number(applied.hue) !== hue.hue) problems.push(`--theme-hue=${applied.hue}，期望 ${hue.hue}`);
  if (Number(applied.saturation) !== hue.saturation) problems.push(`--theme-saturation=${applied.saturation}，期望 ${hue.saturation}`);
  if (applied.dpr !== 1) problems.push(`devicePixelRatio=${applied.dpr}，期望 1`);
  if (applied.viewport[0] !== viewport.width || applied.viewport[1] !== viewport.height) {
    problems.push(`视口 ${applied.viewport.join("x")}，期望 ${viewport.id}`);
  }
  if (problems.length) throw new Error(`变体 ${JSON.stringify(variant)} 没有按预期生效：${problems.join("；")}`);
  return applied;
}

/**
 * 等页面静下来：字体就绪、两帧渲染、DOM 在 quietMs 内没有变化。
 * 超时抛错——页面一直在变说明采集不稳定，要先修采集，不能放宽阈值（viva-ui-development.md §2.4）。
 */
export async function settle(page, { quietMs = 200, timeoutMs = 8000 } = {}) {
  await page.evaluate(async ({ quietMs: quiet, timeoutMs: timeout }) => {
    await document.fonts.ready;
    const nextFrame = () => new Promise((r) => requestAnimationFrame(() => r()));
    await nextFrame();
    await nextFrame();
    await new Promise((resolve, reject) => {
      let last = performance.now();
      const observer = new MutationObserver(() => { last = performance.now(); });
      observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
      const started = performance.now();
      const tick = () => {
        const now = performance.now();
        if (now - last >= quiet) { observer.disconnect(); resolve(); return; }
        if (now - started > timeout) { observer.disconnect(); reject(new Error(`页面在 ${timeout}ms 内没有静下来`)); return; }
        setTimeout(tick, 25);
      };
      tick();
    });
    await nextFrame();
  }, { quietMs, timeoutMs });
}

/**
 * 把鼠标挪到视口外并清掉焦点，避免悬停/焦点样式混进「正常」态场景。
 * 返回仍处于 :hover 的元素数量（除 html/body 外应为 0）。
 */
export async function parkPointer(session, { keepFocus = false } = {}) {
  const { page, cdp } = session;
  await cdp.send("Input.dispatchMouseEvent", { type: "mouseMoved", x: -1, y: -1 });
  // keepFocus：场景的自然状态里焦点就在某个控件上（菜单打开后第一项自动聚焦），清掉它就不是用户看到的画面。
  if (!keepFocus) {
    await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); });
  }
  await settle(page, { quietMs: 80 });
  return page.evaluate(() => [...document.querySelectorAll(":hover")].filter((el) => el !== document.documentElement && el !== document.body).length);
}
