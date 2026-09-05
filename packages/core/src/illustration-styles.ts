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
    description: "白底黑色手绘线稿，用一个动作讲清方法、判断与常见误区",
    group: "通用",
    visualDna:
      "Pure white background with lively black pen lines, slightly uneven and visibly hand-drawn. Use " +
      "a heavier contour for the main subject and finer strokes for supporting objects. Build one " +
      "compact scene around a clear physical action; silhouettes and contact between objects carry the " +
      "explanation before any words are read. Keep roughly a third of the canvas as open background " +
      "and leave comfortable margins. Use one muted orange accent on the decisive object or action; " +
      "add red only when the supplied idea explicitly involves a warning. If labels are supplied, use " +
      "neat, medium-weight Chinese handwriting beside the relevant object, with clear separation from " +
      "the drawing. The result feels like a thoughtful sketch in a working notebook, readable at " +
      "article-column size.",
    character: "",
    negative:
      "No decorative arrows, rainbow annotations, tiny scribbles, paper ruling, paper grain, gradients " +
      "or drop shadows. No slide layout, icon collection, mascot poster, photorealism or interface " +
      "screenshot.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 3,
  },
  {
    id: "ink-sketch",
    name: "淡墨速写",
    description: "白底浓淡墨色与疏朗留白，适合随笔、思考与抽象观点",
    group: "通用",
    visualDna:
      "Clean white background. Draw the main subject in expressive charcoal-black ink contours with a " +
      "few pale grey brush washes to establish volume. Reserve the darkest ink for the action or point " +
      "of contact; supporting objects fade into lighter, simpler strokes. Use one off-centre cluster, " +
      "an asymmetric balance and broad untouched space. Keep the key silhouette recognisable and the " +
      "relationship physically readable, even when details are omitted. Convey restraint through " +
      "selective marks and soft brush edges. If labels are supplied, set them in small but legible " +
      "plain Chinese handwriting in dark ink, separated from the washes. A quiet contemporary " +
      "editorial ink sketch, not a decorative landscape.",
    character: "",
    negative:
      "No colour, ornamental mountains or bamboo added as filler, calligraphy seals, aged-paper " +
      "effects, dense cross-hatching, framing borders, photorealism or elaborate scenery.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 2,
  },
  {
    id: "blueprint-dark",
    name: "深色蓝图",
    description: "深靛蓝底青白线描，一处琥珀强调，适合机制、管线与系统瓶颈",
    group: "技术与系统",
    visualDna:
      "Solid deep indigo background, including all empty space. Draw recognisable physical components " +
      "in crisp cyan-white technical pen lines: stronger outer contours and fewer, finer internal " +
      "details. Arrange a single readable mechanism with visible contact points, openings or " +
      "connections; the viewer should see what passes, blocks or changes. Use one restrained amber " +
      "accent on the decisive component or segment. Keep at least a third of the frame bare indigo, " +
      "with clear separation between parts. Lines must remain visible at article-column size. If " +
      "labels are supplied, use clean upright Chinese lettering in cyan-white beside the relevant " +
      "part. An engineer's conceptual cutaway sketch, with tangible objects rather than a network of " +
      "labelled boxes.",
    character: "",
    negative:
      "No white background, graph-paper grid, dimension lines, blueprint title block, decorative " +
      "circuitry, neon glow, HUD, starfield, gradient background, dashboard, code screenshot or " +
      "metallic rendering.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 4,
  },
  {
    id: "warm-life",
    name: "暖调生活",
    description: "奶油底、陶土橙与鼠尾草绿，适合消费、习惯与日常选择",
    group: "生活与人物",
    visualDna:
      "Warm cream background. Use softly irregular brown outlines and opaque flat gouache-like fills " +
      "in terracotta, muted ochre and sage green. Give the main object a clear, substantial " +
      "silhouette; simplify the supporting objects and keep their shapes familiar. Stage a small " +
      "everyday scene around the supplied action, on one simple ground line or surface when needed. " +
      "Warmth comes from rounded edges and balanced colours rather than decorative accessories. Leave " +
      "broad cream margins and comfortable gaps. If labels are supplied, use legible warm-brown " +
      "Chinese handwriting on clear background beside the object. A calm lifestyle editorial " +
      "illustration with a tactile painted feel and no photographic lighting.",
    character: "",
    negative:
      "No glossy product advertising, brand logos, elaborate packaging text, busy interior decoration, " +
      "stock office characters, exaggerated mascot proportions, watery bleeding, heavy paper grain or " +
      "photorealism.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 3,
  },
  {
    id: "duo-figure",
    name: "双色小人",
    description: "珊瑚红与灰蓝简笔人物，用姿态和距离表现关系、情绪与沟通",
    group: "生活与人物",
    visualDna:
      "Pure white background. Use spare hand-drawn human figures with rounded heads and simple, " +
      "proportionate bodies. One coral figure carries the main action; when a second person is needed, " +
      "use a slate-blue figure. Keep each figure's colour and visual role consistent. Express the idea " +
      "through body direction, reach, distance and interaction with the specified props. Draw props in " +
      "a lighter charcoal outline so they support the action without competing. Maintain an " +
      "easy-to-read silhouette and generous open space around the gesture. If labels are supplied, use " +
      "plain black Chinese handwriting beside the relevant figure or prop. The emotional tone is " +
      "humane and understated.",
    character:
      "A simple coral human figure with a round head, economical limbs and minimal facial detail " +
      "performs the main action. Add a matching slate-blue figure only when the supplied scene " +
      "requires another person. Preserve the same proportions and colours; their posture and " +
      "interaction carry the meaning.",
    negative:
      "No detailed portraits, exaggerated facial expressions, emotion icons, speech bubbles, thought " +
      "clouds, decorative hearts, crowd scenes, oversized mascot heads, dramatic spotlights or " +
      "photorealism.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 2,
  },
  {
    id: "iso-space",
    name: "等距场景",
    description: "白底轻等距立体场景，适合设备摆位、空间关系与部署连接",
    group: "技术与系统",
    visualDna:
      "Pure white background. Use a consistent light isometric view, roughly 30 degrees, with parallel " +
      "edges and no vanishing point. Draw the specified objects as recognisable solid volumes with " +
      "clean charcoal contours, flat warm-grey faces and one muted teal accent on the key object or " +
      "connection. Show only the detail needed to understand position, containment or contact. A " +
      "cutaway may reveal a relevant interior; keep every connection visibly attached to its intended " +
      "endpoints and preserve the supplied direction. Group the scene on a compact footprint with " +
      "generous blank margins. If labels are supplied, place upright dark Chinese lettering on the " +
      "background beside each relevant volume. A precise illustrated miniature, readable without " +
      "decorative technical detail.",
    character: "",
    negative:
      "No glossy 3D rendering, ray tracing, realistic materials, floor grid, floating unconnected " +
      "cables, crowded miniature city, icon-pack layout, gradients, glow or device logos.",
    aspectRatio: "16:9",
    maxShots: 5,
    maxLabels: 4,
  },
  {
    id: "paper-cut",
    name: "剪纸层叠",
    group: "创意与观点",
    description: "米白底、纸片轮廓与浅层叠压，适合边界、筛选与积累类概念",
    visualDna:
      "Warm ivory background. Construct the specified objects from a few large, recognisable cut-paper " +
      "silhouettes in muted teal, ochre and terracotta. Edges show a slight handmade irregularity; two " +
      "or three shallow overlapping layers create depth, with only a delicate contact shadow where " +
      "sheets actually overlap. Use overlap, an opening or a visible point of contact to express the " +
      "supplied action, keeping foreground and background roles unambiguous. Keep one focal cluster, " +
      "broad quiet margins and crisp separation of shapes at article-column size. The medium is matte " +
      "coloured paper, not folded origami or a digital interface. If labels are supplied, use clean " +
      "dark Chinese lettering on an uncluttered background area beside the relevant object.",
    character: "",
    negative:
      "No intricate lace cutting, decorative confetti, tiled colour swatches, glossy plastic, deep " +
      "cast shadows, photographic tabletop, craft tools, greeting-card border or decorative text.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 2,
  },
  {
    id: "editorial-collage",
    name: "编辑拼贴",
    group: "创意与观点",
    description: "暖灰底、单色剪影与朱红强调，适合认知偏差、社会观察与评论",
    visualDna:
      "Flat warm light-grey background. Create a restrained editorial collage from monochrome " +
      "illustrated cutouts of the specified subjects, with a very light halftone texture inside the " +
      "cutouts only. Use one vermilion accent shape to focus attention on the decisive action or " +
      "obstruction. Build a single coherent encounter between objects with deliberate overlap and one " +
      "clearly dominant silhouette. A modest scale shift may clarify the supplied metaphor, but must " +
      "not change factual quantities, ranking or relationships. Keep plenty of plain background and " +
      "avoid a scrapbook layout. If labels are supplied, set them in clear upright dark Chinese " +
      "lettering, separate from the cutouts. The tone is thoughtful, sharp and suitable for a " +
      "long-form essay.",
    character: "",
    negative:
      "No newspaper clippings, ransom-note lettering, torn text fragments, celebrity portraits, " +
      "invented logos, decorative eyes or hands, busy mood-board arrangement, heavy distressing or " +
      "sensational poster typography.",
    aspectRatio: "16:9",
    maxShots: 4,
    maxLabels: 2,
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
