// 窗口状态 / 启动可见性 / 托盘菜单的差分基准：真实的 window-state.ts、tray-menu.ts。
import fs from "node:fs";
import path from "node:path";

export interface WindowGoldenCtx {
  out: string;
}

export async function buildWindowGolden(ctx: WindowGoldenCtx): Promise<unknown> {
  const ws = await import("../../apps/desktop/src/main/window-state.ts");
  const tm = await import("../../apps/desktop/src/main/tray-menu.ts");
  const base = path.join(ctx.out, "win");
  fs.rmSync(base, { recursive: true, force: true });
  fs.mkdirSync(base, { recursive: true });

  const D1 = { x: 0, y: 0, width: 1920, height: 1040 };
  const D2 = { x: 1920, y: 0, width: 1280, height: 984 };
  const DNEG = { x: -1280, y: 100, width: 1280, height: 900 };
  const displaySets: Record<string, typeof D1[]> = {
    one: [D1],
    two: [D1, D2],
    negative: [DNEG, D1],
    none: [],
    small: [{ x: 0, y: 0, width: 900, height: 650 }],
    tiny: [{ x: 0, y: 0, width: 640, height: 400 }],
  };
  const st = (b: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({ version: 1, bounds: b, isMaximized: false, ...extra });
  const persistedCases: Array<{ name: string; value: unknown }> = [
    { name: "null", value: null },
    { name: "string", value: "x" },
    { name: "array", value: [] },
    { name: "empty", value: {} },
    { name: "valid", value: st({ x: 100, y: 80, width: 1200, height: 800 }) },
    { name: "validMax", value: st({ x: 100, y: 80, width: 1200, height: 800 }, { isMaximized: true }) },
    { name: "version2", value: { ...st({ x: 1, y: 1, width: 1000, height: 700 }), version: 2 } },
    { name: "versionString", value: { ...st({ x: 1, y: 1, width: 1000, height: 700 }), version: "1" } },
    { name: "maxNotBool", value: st({ x: 1, y: 1, width: 1000, height: 700 }, { isMaximized: 1 }) },
    { name: "noBounds", value: { version: 1, isMaximized: false } },
    { name: "boundsNull", value: { version: 1, isMaximized: false, bounds: null } },
    { name: "xString", value: st({ x: "1", y: 1, width: 1000, height: 700 }) },
    { name: "xMissing", value: st({ y: 1, width: 1000, height: 700 }) },
    { name: "tooNarrow", value: st({ x: 1, y: 1, width: 799, height: 700 }) },
    { name: "tooShort", value: st({ x: 1, y: 1, width: 1000, height: 599 }) },
    { name: "minExact", value: st({ x: 10, y: 10, width: 800, height: 600 }) },
    { name: "fractional", value: st({ x: 10.5, y: 10.4, width: 1000.5, height: 700.49 }) },
    { name: "negativeFrac", value: st({ x: -10.5, y: -0.5, width: 1000, height: 700 }) },
    { name: "offRight", value: st({ x: 5000, y: 100, width: 1000, height: 700 }) },
    { name: "partlyOffRight", value: st({ x: 1500, y: 700, width: 1000, height: 700 }) },
    { name: "biggerThanDisplay", value: st({ x: 0, y: 0, width: 4000, height: 3000 }) },
    { name: "onSecond", value: st({ x: 2000, y: 50, width: 1000, height: 700 }) },
    { name: "straddle", value: st({ x: 1500, y: 100, width: 800, height: 600 }) },
    { name: "tieOverlap", value: st({ x: 1420, y: 100, width: 1000, height: 600 }) },
    { name: "negativeDisplay", value: st({ x: -1000, y: 200, width: 900, height: 700 }) },
    { name: "extraKeys", value: st({ x: 5, y: 5, width: 1000, height: 700, extra: 1 }, { foo: "bar" }) },
  ];
  const resolve: unknown[] = [];
  for (const p of persistedCases) {
    for (const [dn, ds] of Object.entries(displaySets)) {
      resolve.push({ persisted: p.name, displays: dn, value: p.value, result: ws.resolveWindowLaunchState(p.value, ds) });
    }
  }

  const visCases = [
    { argv: [], openedAtLogin: false, minimizeOnLaunch: false, shouldMaximize: false },
    { argv: [], openedAtLogin: false, minimizeOnLaunch: true, shouldMaximize: true },
    { argv: ["--hidden"], openedAtLogin: false, minimizeOnLaunch: false, shouldMaximize: true },
    { argv: ["--hidden"], openedAtLogin: false, minimizeOnLaunch: false, shouldMaximize: false },
    { argv: ["a", "--hidden", "b"], openedAtLogin: true, minimizeOnLaunch: true, shouldMaximize: true },
    { argv: [], openedAtLogin: true, minimizeOnLaunch: true, shouldMaximize: true },
    { argv: [], openedAtLogin: true, minimizeOnLaunch: true, shouldMaximize: false },
    { argv: [], openedAtLogin: true, minimizeOnLaunch: false, shouldMaximize: true },
    { argv: ["--HIDDEN"], openedAtLogin: false, minimizeOnLaunch: false, shouldMaximize: true },
    { argv: ["--hidden=1"], openedAtLogin: false, minimizeOnLaunch: false, shouldMaximize: true },
  ].map((input) => ({ input, result: ws.resolveLaunchVisibility(input) }));

  // 文件读写：读取（不存在/坏 JSON/有效）、写出字节、capture
  const fileCases: Array<{ name: string; file: string | null }> = [
    { name: "missing", file: null },
    { name: "badJson", file: "{oops" },
    { name: "valid", file: JSON.stringify({ version: 1, bounds: { x: 100, y: 80, width: 1200, height: 800 }, isMaximized: true }) + "\n" },
    { name: "bom", file: "\uFEFF" + JSON.stringify({ version: 1, bounds: { x: 100, y: 80, width: 1200, height: 800 }, isMaximized: true }) },
    { name: "empty", file: "" },
  ];
  const readFile: unknown[] = [];
  for (const c of fileCases) {
    const f = path.join(base, "state-" + c.name + ".json");
    if (c.file !== null) fs.writeFileSync(f, c.file, "utf8");
    readFile.push({ name: c.name, file: c.file, result: ws.readWindowLaunchState(f, [D1]) });
  }

  const captureCases = [
    { name: "normal", bounds: { x: 100, y: 80, width: 1200, height: 800 }, max: false, full: false },
    { name: "maximized", bounds: { x: 100, y: 80, width: 1200, height: 800 }, max: true, full: false },
    { name: "fullscreen", bounds: { x: 0, y: 0, width: 1920, height: 1080 }, max: false, full: true },
    { name: "tooSmall", bounds: { x: 0, y: 0, width: 400, height: 300 }, max: false, full: false },
    { name: "fractional", bounds: { x: 0.5, y: 1.5, width: 1000.5, height: 700.5 }, max: false, full: false },
  ].map((c) => {
    const state = ws.captureWindowState({ getNormalBounds: () => c.bounds, isMaximized: () => c.max, isFullScreen: () => c.full });
    let written: string | null = null;
    if (state) {
      const f = path.join(base, "write-" + c.name + ".json");
      ws.writeWindowState(f, state);
      written = fs.readFileSync(f, "utf8");
    }
    return { ...c, state, written };
  });

  // 托盘菜单：标签、语言归一、模板结构（点击行为用命令记录对拍）
  const locales = ["en", "en-US", "zh", "zh-CN", "zh-Hant-TW", "ZH-cn", " zh ", "ja", "fr-FR", "", "z", "zhx", "中文"];
  const labels = locales.map((l) => ({ locale: l, language: tm.normalizeTrayMenuLanguage(l), labels: tm.getTrayMenuLabels(l) }));
  const templates = [true, false].flatMap((visible) =>
    ["en", "zh"].map((lang) => {
      const events: string[] = [];
      const tpl = tm.buildTrayMenuTemplate({
        agentManagementEnabled: false,
        isWindowVisible: visible,
        labels: tm.getTrayMenuLabels(lang),
        onCommand: (c: { type: string }) => events.push("cmd:" + c.type),
        onQuit: () => events.push("quit"),
        onToggleWindow: () => events.push("toggle"),
      });
      const items = tpl.map((it) => {
        const before = events.length;
        (it as { click?: () => void }).click?.();
        return { label: it.label ?? null, type: it.type ?? null, event: events.length > before ? events[events.length - 1] : null };
      });
      return { visible, lang, items };
    }),
  );

  return { resolve, visCases, readFile, captureCases, labels, templates, supported: tm.SUPPORTED_TRAY_MENU_LANGUAGES };
}
