/**
 * 场景的外观/视口展开矩阵（docs/agents/viva-ui-development.md §2.1）与运行档位（§2.6）。
 *
 * 色相预设与字号档不在这里抄一份，直接读渲染层 stores/settings/settings-appearance.ts（唯一来源）；
 * 视口是对等定义里固定的三个。
 */
import fs from "node:fs";
import path from "node:path";
import { REPO_ROOT } from "./paths.mjs";

const APPEARANCE_SRC = path.join(REPO_ROOT, "apps/desktop/src/renderer/stores/settings/settings-appearance.ts");

export const VIEWPORTS = [
  { id: "1280x800", width: 1280, height: 800 },
  { id: "800x600", width: 800, height: 600 },
  { id: "1920x1080", width: 1920, height: 1080 },
];
export const MODES = ["light", "dark"];

export function parseAppearanceSource(source) {
  const hues = [...source.matchAll(/\{\s*id:\s*"([^"]+)",\s*hue:\s*(\d+),\s*saturation:\s*(\d+)/g)].map((m) => ({
    id: m[1], hue: Number(m[2]), saturation: Number(m[3]),
  }));
  const fonts = [...source.matchAll(/\{\s*id:\s*"([^"]+)",\s*value:\s*(\d+),\s*name:/g)].map((m) => ({ id: m[1], px: Number(m[2]) }));
  if (hues.length === 0) throw new Error("settings-appearance.ts 里找不到 MORANDI_THEMES");
  if (fonts.length === 0) throw new Error("settings-appearance.ts 里找不到 FONT_SIZES");
  return { hues, fonts };
}

export function loadMatrix() {
  const { hues, fonts } = parseAppearanceSource(fs.readFileSync(APPEARANCE_SRC, "utf8"));
  return { modes: MODES, hues, fonts, viewports: VIEWPORTS };
}

export const DEFAULTS = { mode: "light", hue: "royal-blue", font: "medium", viewport: "1280x800" };

export function variantKey(v) {
  return `${v.mode}-${v.hue}-${v.font}-${v.viewport}`;
}

export function parseVariantKey(key, matrix = loadMatrix()) {
  for (const mode of matrix.modes) {
    if (!key.startsWith(`${mode}-`)) continue;
    const rest = key.slice(mode.length + 1);
    for (const vp of matrix.viewports) {
      if (!rest.endsWith(`-${vp.id}`)) continue;
      const mid = rest.slice(0, rest.length - vp.id.length - 1);
      for (const font of matrix.fonts) {
        if (!mid.endsWith(`-${font.id}`)) continue;
        const hue = mid.slice(0, mid.length - font.id.length - 1);
        if (matrix.hues.some((h) => h.id === hue)) return { mode, hue, font: font.id, viewport: vp.id };
      }
    }
  }
  throw new Error(`无法解析变体 ${key}（形如 light-royal-blue-medium-1280x800）`);
}

/** 运行档位（§2.6）：只是运行方式，不改变完成的定义。 */
export function tierVariants(tier, matrix = loadMatrix()) {
  const base = DEFAULTS;
  const all = (vs) => vs.map((v) => ({ ...base, ...v }));
  switch (tier) {
    case "T0":
      return all(matrix.modes.map((mode) => ({ mode })));
    case "T1":
      return all([
        ...matrix.modes.map((mode) => ({ mode })),
        ...matrix.viewports.filter((vp) => vp.id !== base.viewport).map((vp) => ({ viewport: vp.id })),
      ]);
    case "T2":
      return all(matrix.modes.flatMap((mode) => matrix.hues.flatMap((h) => matrix.fonts.map((f) => ({ mode, hue: h.id, font: f.id })))));
    case "T3":
      return all(matrix.modes.flatMap((mode) => matrix.hues.flatMap((h) => matrix.fonts.flatMap((f) => matrix.viewports.map((vp) => ({ mode, hue: h.id, font: f.id, viewport: vp.id }))))));
    default:
      throw new Error(`未知档位 ${tier}（T0|T1|T2|T3）`);
  }
}
