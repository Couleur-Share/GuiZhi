/**
 * 正文配图的风格预设。
 *
 * 风格是数据不是代码：内置预设只是首次运行时播下的种子，之后这份
 * `config/illustration-styles.json` 就归用户所有——改画法、换配色、
 * 加一个每张图都出场的固定角色，都不需要改代码，也不会被升级覆盖。
 */
import fs from "fs";
import path from "path";

import { getConfigDir } from "./runtime-paths";
import type {
  IllustrationAspectRatio,
  IllustrationStyle,
} from "@guizhi/shared/types";

const STYLES_FILE_NAME = "illustration-styles.json";
const STYLES_FILE_VERSION = 2;
const ASPECT_RATIOS: IllustrationAspectRatio[] = ["16:9", "4:3", "1:1"];
const MAX_SHOTS_LIMIT = 12;
const MAX_LABELS_LIMIT = 10;

/** 写进预设文件的字段说明：JSON 没有注释，只能占一个字段 */
const STYLES_README = [
  "归知正文配图的风格预设。改完保存即生效，无需重启。",
  "visualDna：画法、配色、留白，会原样拼进生图提示词（写英文对图像模型更稳）。",
  "character：每张图都要出场并承担核心动作的固定角色；留空则画面里不要求有角色。",
  "negative：明确排除的观感，同样拼进提示词。",
  "maxLabels：单张图最多几处文字标注。图像模型写中文容易出错字，标注越少越稳。",
  "group：选择器里的分组名，自由文本，留空则排在最前。",
  "dismissedBuiltIns：被你删掉的内置风格 id。应用更新带来的新内置风格会自动补进 styles，" +
    "但这个名单里的不会再回来——想要回来，删掉对应的 id 即可。",
];

/**
 * 内置预设。
 *
 * `negative` 里避开 anime / manga / chibi / children's book 这类词：图像接口
 * 没有 negative 参数，这段话是拼进正向提示词的，安全分类器读到的是这些词本身
 * 而不是「不要它们」，而它们恰好挨着最容易误伤的那几类。要表达同一个意思，
 * 就换个不带这些词的说法。改这里之前先看一眼这条。
 */
export const BUILT_IN_ILLUSTRATION_STYLES: IllustrationStyle[] = [
  {
    id: "hand-note",
    name: "手绘笔记",
    description: "白底手绘线稿 + 少量彩色手写批注，适合方法、流程与观点类长文",
    group: "通用",
    visualDna:
      "Pure white background. Minimalist black hand-drawn line art with slightly wobbly, " +
      "uneven pen strokes — visibly drawn by hand, never vector-perfect. Generous empty space: " +
      "the subject occupies roughly 40%-60% of the canvas and at least a third stays blank. " +
      "A few short handwritten Chinese annotations sit beside what they describe — black for the " +
      "main subject and its parts, orange for the main flow or path arrows, red only for problems " +
      "and warnings, blue only for secondary notes or system state. The feel is a quick explanatory " +
      "sketch on a blank sheet of paper: restrained, a little odd, clear at a glance.",
    character: "",
    negative:
      "No gradients, drop shadows, paper texture, grain or tinted background. No commercial vector " +
      "illustration, no PPT infographic, no formal flowchart, no course slide, no cute mascot poster, " +
      "no picture-book illustration, no photorealism, no user-interface screenshot.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 5,
  },
  {
    id: "ink-sketch",
    name: "淡墨速写",
    description: "灰阶墨线与淡墨块，安静克制，适合随笔与概念性内容",
    group: "通用",
    visualDna:
      "Plain white background with no paper texture. Grey-scale ink brush lines of varying weight, " +
      "with a few soft washed ink tones for mass; no colour at all. Very sparse composition with wide " +
      "breathing room and the subject sitting off-centre. A handful of short Chinese annotations " +
      "written in the same ink, small and unobtrusive. Quiet and contemplative — closer to a margin " +
      "sketch than to a diagram.",
    character: "",
    negative:
      "No colour, no gradients, no drop shadows, no scanned-page or aged-paper look, no calligraphy " +
      "seals or stamps, no framing border, no dense detail, no commercial illustration polish, " +
      "no photorealism, no ruled axes or boxed layouts.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 4,
  },
  {
    id: "blueprint-dark",
    name: "深色蓝图",
    description: "深靛蓝底 + 青白细线 + 一处琥珀高亮，适合系统、管线与网络链路",
    group: "技术与系统",
    visualDna:
      "Solid deep indigo background, flat and even. Everything is drawn in thin cyan-white " +
      "technical pen lines with slightly uneven, hand-ruled strokes — precise but visibly drawn " +
      "by hand, never vector-perfect. One warm amber accent marks the single most important path " +
      "or object in the scene; nothing else carries colour. The breathing room is the dark " +
      "background itself: at least a third of the canvas stays bare indigo and the subject sits " +
      "in the middle band. Short Chinese annotations in the same cyan-white, small, tucked beside " +
      "what they describe. The feel is an engineer's drawing done in white pen on dark paper: " +
      "calm, exact, with exactly one bright thing worth looking at.",
    character: "",
    negative:
      "Never a white or light background — the empty space in this style is the dark indigo " +
      "itself. No neon glow, bloom, lens flare or sci-fi HUD. No circuit-board traces, no grid or " +
      "graph paper, no starfield, no gradient or vignette background. No 3D render, no glass or " +
      "metal material, no dashboard, no code editor screenshot, no second accent colour.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 6,
  },
  {
    id: "warm-life",
    name: "暖调生活",
    description: "米白底 + 赭橙柔色块，物件画得有温度，适合健康、消费与日常决策",
    group: "生活与人物",
    visualDna:
      "Warm off-white paper-cream background. Everyday objects drawn with a soft brown ink " +
      "outline of even weight and filled with flat, muted warm washes — terracotta, mustard, " +
      "sage green, dusty clay — three or four colours at most, with no shading inside a fill. " +
      "Objects are drawn slightly larger than life and a little rounded, resting on a simple " +
      "ground line rather than floating. Wide margins and at most four or five objects in the " +
      "whole picture. Short Chinese annotations in soft brown handwriting beside the object they " +
      "name. The feel is a spot illustration from a lifestyle magazine: warm, unhurried, plainly " +
      "explanatory.",
    character: "",
    negative:
      "No cold blue or grey palette, no clinical pure-white product shot, no glossy advertising " +
      "render, no photorealism, no watercolour bleeding or paper grain texture, no cute mascot or " +
      "big-headed cartoon character, no stock-illustration office people, no brand names or " +
      "packaging logos.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 5,
  },
  {
    id: "duo-figure",
    name: "双色小人",
    description: "白底 + 两个简笔小人，靠姿态与距离说事，适合关系、情绪与沟通",
    group: "生活与人物",
    visualDna:
      "Pure white background. One or two extremely simple hand-drawn figures carry the whole " +
      "scene: round head, a few confident strokes for body and limbs, no facial features beyond " +
      "a single short line for the mouth. The first figure is warm coral, the second — when the " +
      "idea involves another person — is cool slate blue, so the two are told apart at a glance. " +
      "Everything else (props, furniture, doorways) is a thin black outline with no fill. The " +
      "meaning comes from posture, distance and which way each figure faces: turned away, leaning " +
      "in, one of them behind something. Very sparse, at least half the canvas empty. Short " +
      "Chinese annotations in black, small, beside the figure or object they describe.",
    character:
      "A simple round-headed figure drawn in a few strokes, warm coral, with no facial features " +
      "beyond a single mouth line. It performs the core action itself. When the idea is about a " +
      "relationship, a second figure drawn the same way in cool slate blue appears, and the " +
      "distance and body angle between the two carries the point.",
    negative:
      "No detailed faces or eyes, no comic-book styling, no big-headed mascot, no emoji-like " +
      "expressions, no speech bubbles or thought clouds, no hearts or other emotion symbols, " +
      "no crowd scenes, no gradients or drop shadows, no photorealism.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 4,
  },
  {
    id: "iso-space",
    name: "等距场景",
    description: "白底轻等距视角，画得清设备、房间与它们之间的连接，适合网络与部署",
    group: "技术与系统",
    visualDna:
      "Pure white background. A light isometric view at roughly 30 degrees with no vanishing " +
      "point: objects, rooms and devices are simple solid volumes with clean black outlines and " +
      "flat fills — two warm greys plus a single teal accent on whatever the sentence is really " +
      "about. They are recognisable things seen from above-left, not icons: a router is a box " +
      "with two antennas, a home is a plain shell with one wall removed. Connections are drawn as " +
      "plain lines or tubes that visibly begin and end on an object. Generous empty white around " +
      "the arrangement and at most five volumes in the picture. Short Chinese labels in black " +
      "beside the volume they name.",
    character: "",
    negative:
      "No 3D render, ray tracing, ambient occlusion or realistic materials. No perspective with a " +
      "vanishing point, no floor grid or isometric tile pattern, no crowded isometric-city " +
      "poster, no icon-pack clip art, no gradients, no drop shadows, no glow, no second accent " +
      "colour, no brand logos on devices.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 6,
  },
];

export function getIllustrationStylesFilePath(): string {
  return path.join(getConfigDir(), STYLES_FILE_NAME);
}

function clamp(value: unknown, fallback: number, max: number): number {
  const parsed = Math.trunc(Number(value));
  if (!Number.isFinite(parsed) || parsed < 1) {
    return fallback;
  }
  return Math.min(parsed, max);
}

function readText(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  return typeof value === "string" ? value.trim() : "";
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function normalizeStyle(raw: unknown): IllustrationStyle | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return null;
  }
  const source = raw as Record<string, unknown>;
  const id = readText(source, "id");
  const visualDna = readText(source, "visualDna");
  if (!id || !visualDna) {
    return null;
  }
  const aspectRatio = source.aspectRatio;
  return {
    id,
    name: readText(source, "name") || id,
    description: readText(source, "description"),
    group: readText(source, "group"),
    visualDna,
    character: readText(source, "character"),
    negative: readText(source, "negative"),
    aspectRatio: ASPECT_RATIOS.includes(aspectRatio as IllustrationAspectRatio)
      ? (aspectRatio as IllustrationAspectRatio)
      : "16:9",
    maxShots: clamp(source.maxShots, 5, MAX_SHOTS_LIMIT),
    maxLabels: clamp(source.maxLabels, 5, MAX_LABELS_LIMIT),
  };
}

/**
 * 写入前的严格校验。
 *
 * 读文件时可以静默丢掉坏条目（用户手改 JSON 改坏了，不该连累整个功能），
 * 但按下保存是主动操作，必须说得出是哪一条不行——`normalizeStyle` 一律返回
 * null，分不清是缺 id 还是缺画法，界面上就只能给一句无从下手的「保存失败」。
 */
function validateStyle(
  raw: unknown,
  index: number,
): { style?: IllustrationStyle; error?: string } {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return { error: `第 ${index + 1} 项不是有效的风格` };
  }
  const source = raw as Record<string, unknown>;
  const label =
    readText(source, "name") || readText(source, "id") || `第 ${index + 1} 项`;
  if (!readText(source, "id")) {
    return { error: `${label}：缺少 id` };
  }
  if (!readText(source, "visualDna")) {
    return { error: `${label}：画法与配色不能为空` };
  }
  const style = normalizeStyle(raw);
  return style ? { style } : { error: `${label}：无法解析` };
}

/** 同名 id 会让选择器选中错的那条，撞了就顺延一个后缀 */
function uniqueId(id: string, used: Set<string>): string {
  if (!used.has(id)) {
    return id;
  }
  let suffix = 2;
  while (used.has(`${id}-${suffix}`)) {
    suffix += 1;
  }
  return `${id}-${suffix}`;
}

function serialize(
  styles: IllustrationStyle[],
  dismissedBuiltIns: string[],
): string {
  return `${JSON.stringify(
    {
      kind: "guizhi-illustration-styles",
      version: STYLES_FILE_VERSION,
      readme: STYLES_README,
      dismissedBuiltIns,
      styles,
    },
    null,
    2,
  )}\n`;
}

/**
 * 用户主动删掉的内置风格。
 *
 * 每次保存按「内置里当前列表没有的那些」重算，不做累加：这样它自我纠正——
 * 点一次「恢复内置预设」再保存，名单自然清空，不会留下一条永远拉黑的记录。
 */
function computeDismissed(styles: IllustrationStyle[]): string[] {
  const present = new Set(styles.map((style) => style.id));
  return BUILT_IN_ILLUSTRATION_STYLES.filter(
    (style) => !present.has(style.id),
  ).map((style) => style.id);
}

function readDismissed(parsed: { dismissedBuiltIns?: unknown }): string[] {
  return Array.isArray(parsed.dismissedBuiltIns)
    ? parsed.dismissedBuiltIns.filter(
        (id): id is string => typeof id === "string",
      )
    : [];
}

export interface IllustrationStyleWriteResult {
  success: boolean;
  error?: string;
  /** 落盘后的规范化结果，界面直接拿它回显 */
  styles?: IllustrationStyle[];
}

export class CoreIllustrationStyleService {
  /**
   * 读取风格预设。
   *
   * 文件不存在时以内置预设播种一份——用户打开它就能看到完整字段并照着改。
   * 文件存在但一条都解析不出来时返回内置预设兜底，但**不覆写文件**：
   * 那多半是用户改坏了 JSON，覆盖等于把他写的东西吃掉。
   *
   * 版本升级新增的内置风格在这里增量补进用户的文件。此前只有「首次播种 +
   * 恢复内置预设（整份覆盖）」两条路，新预设要么到不了老用户手上，要么
   * 得让人做一次会冲掉自己改动的破坏性操作。删过的记在 dismissedBuiltIns
   * 里不会复活；名单是每次保存重算的，所以「恢复内置预设」之后它自然清空。
   */
  read(): IllustrationStyle[] {
    const filePath = getIllustrationStylesFilePath();
    if (!fs.existsSync(filePath)) {
      this.seed();
      return BUILT_IN_ILLUSTRATION_STYLES;
    }
    try {
      const parsed = JSON.parse(fs.readFileSync(filePath, "utf8")) as {
        styles?: unknown;
        dismissedBuiltIns?: unknown;
      };
      const styles = Array.isArray(parsed.styles)
        ? parsed.styles
            .map(normalizeStyle)
            .filter((style): style is IllustrationStyle => style !== null)
        : [];
      if (styles.length === 0) {
        return BUILT_IN_ILLUSTRATION_STYLES;
      }
      return this.backfillBuiltIns(styles, readDismissed(parsed));
    } catch (error) {
      console.warn("[illustration] 风格预设文件无法解析，改用内置预设:", error);
      return BUILT_IN_ILLUSTRATION_STYLES;
    }
  }

  /** 补齐缺失的内置风格并落盘；没得补时原样返回，不白写一次文件 */
  private backfillBuiltIns(
    styles: IllustrationStyle[],
    dismissed: string[],
  ): IllustrationStyle[] {
    const known = new Set([...styles.map((style) => style.id), ...dismissed]);
    const missing = BUILT_IN_ILLUSTRATION_STYLES.filter(
      (style) => !known.has(style.id),
    );
    if (missing.length === 0) {
      return styles;
    }
    const merged = [...styles, ...missing];
    try {
      this.persist(merged, dismissed);
    } catch (error) {
      // 写不进去也不影响这一次使用，下次读还会再试
      console.warn("[illustration] 补齐内置风格失败:", error);
    }
    return merged;
  }

  /** 确保文件存在并返回路径（供「在文件夹中显示」定位） */
  ensureFile(): string {
    const filePath = getIllustrationStylesFilePath();
    if (!fs.existsSync(filePath)) {
      this.seed();
    }
    return filePath;
  }

  /** 应用内编辑器回写整份列表 */
  write(raw: unknown): IllustrationStyleWriteResult {
    if (!Array.isArray(raw) || raw.length === 0) {
      return { success: false, error: "至少要保留一套配图风格" };
    }
    const styles: IllustrationStyle[] = [];
    const usedIds = new Set<string>();
    for (let index = 0; index < raw.length; index += 1) {
      const { style, error } = validateStyle(raw[index], index);
      if (error || !style) {
        return { success: false, error: error ?? `第 ${index + 1} 项无法保存` };
      }
      style.id = uniqueId(style.id, usedIds);
      usedIds.add(style.id);
      styles.push(style);
    }
    try {
      this.persist(styles, computeDismissed(styles));
    } catch (error) {
      return { success: false, error: describeError(error) };
    }
    return { success: true, styles };
  }


  /** 按 id 取预设；找不到时退回第一条，保证功能不会因为一个坏 id 卡死 */
  find(id: string | undefined): IllustrationStyle | null {
    const styles = this.read();
    if (styles.length === 0) {
      return null;
    }
    return styles.find((style) => style.id === id) ?? styles[0];
  }

  /** 播种是后台行为，失败只记一条 warn；用户主动保存走 persist，错误要抛出去 */
  private seed(): void {
    try {
      this.persist(BUILT_IN_ILLUSTRATION_STYLES, []);
    } catch (error) {
      console.warn("[illustration] 写入内置风格预设失败:", error);
    }
  }

  private persist(styles: IllustrationStyle[], dismissed: string[]): void {
    fs.mkdirSync(getConfigDir(), { recursive: true });
    fs.writeFileSync(
      getIllustrationStylesFilePath(),
      serialize(styles, dismissed),
      "utf8",
    );
  }
}

export const coreIllustrationStyleService = new CoreIllustrationStyleService();
