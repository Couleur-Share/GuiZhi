/**
 * 配图策划与生图的提示词构造，以及策划输出的确定性解析。
 *
 * 两段式：文本模型先读正文挑「认知锚点」、产出 shot list（严格 JSON），
 * 图像模型再按风格预设逐张生成。放在 shared 里是为了能脱离 Electron 单测——
 * 提示词协议一改，解析这侧必须跟着一起测。
 */
import type {
  IllustrationShot,
  IllustrationStyle,
} from "../types/illustration";
import type { ContentBlock } from "./illustration-note";

/** 单个段落进提示词的字数上限：配图只需要抓住段落主旨 */
const BLOCK_MAX_CHARS = 400;
/** 全部候选段落的总字数上限，超出的尾部段落不进提示词 */
const BLOCKS_MAX_CHARS = 12_000;
const TOPIC_MAX_CHARS = 24;
const LABEL_MAX_CHARS = 12;
const ELEMENT_MAX_CHARS = 16;
const MAX_ELEMENTS = 6;
const COMPOSITION_MAX_CHARS = 200;

export const ILLUSTRATION_PLAN_SYSTEM_PROMPT =
  "你是文章配图策划助手。用户会给出一篇文章的标题、可配图的正文段落（每段带序号）" +
  "与本次使用的插画风格，请挑出最值得配图的位置，为每张图写一份配图规格。\n" +
  "配图的作用是给出文字给不了的那一层直觉，不是把文字再说一遍。\n" +
  "【挑位置】\n" +
  "1. 不要平均配图。只挑「认知锚点」：核心判断、从输入到输出的闭环、前后对比、" +
  "分类分流、关键取舍、常见误区、结论落点。铺垫段、举例段不配图；\n" +
  "2. 一张图只讲一个意思，不要把两件事塞进同一张；\n" +
  "3. 用户会给出本次要几张，**输出恰好那么多**，不要多也不要少。" +
  "锚点比要求的多就挑最值得画的几个，看着不够就把范围放宽到次一级的判断，" +
  "但不要为了凑数把同一个意思拆成两张；\n" +
  "【想画面 —— 这一步最容易做砸】\n" +
  "4. 画的是一个**具体场景**，不是图表。原文里已经写成矩阵、象限、表格、清单、" +
  "坐标系的东西，绝对不要照着再画一遍——那样这张图除了占地方什么也没给。" +
  "要把同一个意思换成眼睛能直接看懂的物理情境：分拣就真的把东西丢进几个箱子，" +
  "取舍就画天平和秤，过滤就画漏斗，卡住就画卡在缝里，积累就画一层层码起来；\n" +
  "5. 画面里出现的具体物件必须取自原文——原文举了哪些例子就画哪些。" +
  "自己编的例子经常和原文自相矛盾（原文说手机要买好的，图里却把手机画进了" +
  "「谨慎购买」那一格），这是最严重的错误；\n" +
  "6. 同一篇里不要重复用同一个隐喻；\n" +
  "7. 标注词直接画在图上，每条 2~8 字，能读、不啰嗦。\n" +
  "【顺带挑风格】\n" +
  "8. 用户会给出可选的插画风格清单。你已经读完了正文，如果其中某一套明显比" +
  "当前选中的更配这篇内容，把它的 id 填进 styleId；当前这套就合适、或你拿不准，" +
  "就把 styleId 留空。这只是建议，用户点了才会换。\n" +
  "输出：严格的 JSON 对象 `{\"styleId\": \"<建议的风格 id，没有就空字符串>\", " +
  '"shots": [ ... ]}`，不要代码围栏，不要任何解释文字。shots 的每个元素：\n' +
  '{"afterBlock": <整数，这张图插在哪个序号的段落之后，必须取自用户给出的候选序号，不得重复>, ' +
  '"topic": "<图题，8~16 字，会成为图片 alt 与检索文本>", ' +
  '"coreIdea": "<一句话说清这张图要表达什么>", ' +
  '"scene": "<物理情境，取值：分拣 / 衡量取舍 / 过滤漏斗 / 加工流水线 / 前后对比 / ' +
  '卡住与通过 / 层层堆叠 / 小场景分镜>", ' +
  '"composition": "<具体画面：画面里有什么、谁在做什么动作、东西从哪去哪，30~80 字>", ' +
  '"elements": ["<画面里要出现的具体物件，取自原文的例子>", ...], ' +
  '"labels": ["<标注词>", ...]}';

/**
 * 与风格无关的硬约束，拼在每张生图提示词里。
 *
 * 刻意放在代码里而不是风格预设的 negative 字段：预设文件首次运行就落到用户机器上、
 * 之后不再被覆盖，写在那里的修正到不了已经在用的人手上。何况「要插画不要图表」
 * 是这个功能本身的立场，不是某一套画风的偏好。
 */
const BASE_IMAGE_CONSTRAINTS = [
  "This is an illustration, not a chart. Do not draw coordinate axes, a 2x2 matrix, " +
    "a quadrant layout, a grid of cells, a table, a bar or line chart, a formal flowchart " +
    "or an org chart. The meaning must be carried by concrete objects and a physical action " +
    "inside a single scene.",
  "One image explains only one idea. Keep the composition sparse and leave generous white space.",
];

function truncate(text: string, max: number): string {
  const normalized = text.trim();
  return normalized.length > max ? `${normalized.slice(0, max)}…` : normalized;
}

/** 候选段落清单：`[序号] 内容`，超出总预算的尾部段落丢弃 */
export function formatAnchorBlocks(blocks: ContentBlock[]): string {
  const lines: string[] = [];
  let budget = BLOCKS_MAX_CHARS;
  for (const block of blocks) {
    const text = truncate(block.text.replace(/\n+/g, " "), BLOCK_MAX_CHARS);
    if (text.length > budget) {
      break;
    }
    budget -= text.length;
    lines.push(`[${block.index}] ${text}`);
  }
  return lines.join("\n");
}

/**
 * 「自动」档的目标张数：约每 3 个可配图段落配一张。
 *
 * 从文章本身算，所以同一篇每次都得到同一个数——此前只给「最多 N 张」加一句
 * 「宁少勿滥」，中间完全自由，同一篇文章两次策划会给出 3 张和 4 张。
 */
export function deriveShotTarget(
  anchorCount: number,
  maxShots: number,
): number {
  if (anchorCount <= 0) {
    return 0;
  }
  return Math.max(1, Math.min(Math.round(anchorCount / 3), maxShots, anchorCount));
}

/** 可选风格清单：模型已经在读正文了，顺带让它看一眼有哪些风格可选 */
function formatStyleCatalog(
  catalog: IllustrationStyle[],
  currentId: string,
): string[] {
  const others = catalog.filter((style) => style.id !== currentId);
  if (others.length === 0) {
    return [];
  }
  return [
    "",
    "还可以选的插画风格（id — 名称 — 适合什么内容）：",
    ...others.map(
      (style) => `${style.id} — ${style.name} — ${style.description}`,
    ),
  ];
}

export function buildIllustrationPlanPrompt(input: {
  title: string;
  style: IllustrationStyle;
  blocks: ContentBlock[];
  targetShots: number;
  /** 全部可选风格；缺省则不请模型给建议 */
  catalog?: IllustrationStyle[];
}): string {
  return [
    `文章标题：《${input.title.trim() || "无标题"}》`,
    `插画风格：${input.style.name}——${input.style.description}`,
    `本次要 ${input.targetShots} 张图（恰好这么多），每张最多 ${input.style.maxLabels} 条标注词。`,
    ...formatStyleCatalog(input.catalog ?? [], input.style.id),
    "",
    "可配图的段落（序号 → 内容）：",
    formatAnchorBlocks(input.blocks),
  ].join("\n");
}

/** 去掉包裹整段输出的 ``` 围栏（模型常无视「不要代码围栏」） */
function stripCodeFence(text: string): string {
  const lines = text.trim().split("\n");
  if (
    lines.length >= 2 &&
    /^```[a-zA-Z]*\s*$/.test(lines[0]) &&
    lines[lines.length - 1].trim() === "```"
  ) {
    return lines.slice(1, -1).join("\n").trim();
  }
  return text.trim();
}

function parseJsonSlice(text: string, open: string, close: string): unknown {
  const start = text.indexOf(open);
  const end = text.lastIndexOf(close);
  if (start < 0 || end <= start) {
    return null;
  }
  try {
    return JSON.parse(text.slice(start, end + 1));
  } catch {
    return null;
  }
}

interface PlanPayload {
  styleId: string;
  entries: unknown[];
}

/**
 * 抽出策划输出。
 *
 * 协议是 `{styleId, shots}` 对象，但裸数组同样收下——这层兼容不是为了旧数据，
 * 是因为模型本来就经常无视格式指令，为此整次策划失败不值得。
 * 先试对象再退回数组：单条 shot 的裸数组切出来也是个合法对象，
 * 靠「有没有 shots 数组」这一条区分。
 */
function extractPlanPayload(raw: string): PlanPayload {
  const text = stripCodeFence(raw);
  const object = parseJsonSlice(text, "{", "}");
  if (object && typeof object === "object" && !Array.isArray(object)) {
    const source = object as Record<string, unknown>;
    if (Array.isArray(source.shots)) {
      return { styleId: readString(source, "styleId"), entries: source.shots };
    }
  }
  const array = parseJsonSlice(text, "[", "]");
  return { styleId: "", entries: Array.isArray(array) ? array : [] };
}

function readString(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  return typeof value === "string" ? value.trim() : "";
}

function readStringList(
  value: unknown,
  maxChars: number,
  maxCount: number,
): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter((entry): entry is string => typeof entry === "string")
    .map((entry) => truncate(entry, maxChars))
    .filter(Boolean)
    .slice(0, maxCount);
}

/**
 * 模型给的序号不在候选里时贴到最近的候选，而不是整条丢弃。
 *
 * 贴的是最近的**尚未被占用**的候选：两条 shot 落到同一段时，若只按「最近」
 * 贴，后一条会因为撞号被整条丢掉——模型本来出了 4 张，用户拿到 3 张，
 * 而且界面上看不出少了什么，只会觉得张数飘忽不定。
 */
function snapToAllowed(
  value: number,
  allowed: number[],
  used: Set<number>,
): number | null {
  const free = allowed.filter((block) => !used.has(block));
  if (free.length === 0) {
    return null;
  }
  if (free.includes(value)) {
    return value;
  }
  return free.reduce((best, candidate) =>
    Math.abs(candidate - value) < Math.abs(best - value) ? candidate : best,
  );
}

export interface ParseIllustrationShotsOptions {
  /** 允许的段落序号（listAnchorBlocks 的结果） */
  allowedBlocks: number[];
  maxShots: number;
  maxLabels: number;
}

export interface IllustrationPlanParseResult {
  /** 模型建议改用的风格 id；没给建议时是空串 */
  styleId: string;
  shots: IllustrationShot[];
}

/**
 * 解析策划输出。
 *
 * 模型的排版与字段纪律都靠不住，这层清洗不能省：序号贴回候选集合、
 * 去重、限长、限张数。任何一条不合格只丢这一条，不让整次策划失败。
 */
export function parseIllustrationPlan(
  raw: string,
  options: ParseIllustrationShotsOptions,
): IllustrationPlanParseResult {
  const payload = extractPlanPayload(raw);
  return { styleId: payload.styleId, shots: cleanShots(payload.entries, options) };
}

/** 只要 shot list 的那条路：面板回传的规格复用同一套清洗 */
export function parseIllustrationShots(
  raw: string,
  options: ParseIllustrationShotsOptions,
): IllustrationShot[] {
  return parseIllustrationPlan(raw, options).shots;
}

function cleanShots(
  parsed: unknown[],
  options: ParseIllustrationShotsOptions,
): IllustrationShot[] {
  const shots: IllustrationShot[] = [];
  const used = new Set<number>();

  for (const entry of parsed) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      continue;
    }
    const source = entry as Record<string, unknown>;
    const rawBlock = Number(source.afterBlock);
    if (!Number.isFinite(rawBlock)) {
      continue;
    }
    const afterBlock = snapToAllowed(
      Math.trunc(rawBlock),
      options.allowedBlocks,
      used,
    );
    if (afterBlock === null) {
      continue;
    }
    const topic = readString(source, "topic");
    if (!topic) {
      continue;
    }

    used.add(afterBlock);
    shots.push({
      afterBlock,
      topic: truncate(topic, TOPIC_MAX_CHARS),
      coreIdea: readString(source, "coreIdea"),
      scene: readString(source, "scene"),
      composition: truncate(
        readString(source, "composition"),
        COMPOSITION_MAX_CHARS,
      ),
      elements: readStringList(
        source.elements,
        ELEMENT_MAX_CHARS,
        MAX_ELEMENTS,
      ),
      labels: readStringList(source.labels, LABEL_MAX_CHARS, options.maxLabels),
    });
    if (shots.length >= options.maxShots) {
      break;
    }
  }

  // 按正文顺序返回，面板里的排列才与阅读顺序一致
  return shots.sort((a, b) => a.afterBlock - b.afterBlock);
}

/**
 * 单张生图提示词。
 *
 * 英文骨架 + 中文内容是刻意的：图像模型对英文的风格指令更敏感，
 * 而要画在图上的文字必须原样给中文，翻译过去就画错了。
 */
export function buildIllustrationImagePrompt(
  style: IllustrationStyle,
  shot: IllustrationShot,
): string {
  const parts = [
    `Generate one standalone ${style.aspectRatio} horizontal illustration for a Chinese article.`,
    "",
    "Visual style:",
    style.visualDna.trim(),
  ];

  const character = style.character.trim();
  if (character) {
    parts.push(
      "",
      "Recurring character (must perform the core action of the idea, never stand beside it as decoration):",
      character,
    );
  }

  parts.push("", `Theme: ${shot.topic}`);
  if (shot.coreIdea) {
    parts.push(`Core idea: ${shot.coreIdea}`);
  }
  if (shot.scene) {
    parts.push(`Physical situation: ${shot.scene}`);
  }
  if (shot.composition) {
    parts.push(`Composition: ${shot.composition}`);
  }
  if (shot.elements.length > 0) {
    // 物件清单来自原文，模型自己另编例子就会和正文打架
    parts.push(
      `Objects that must appear, and no other subject matter: ${shot.elements.join(" / ")}`,
    );
  }
  if (shot.labels.length > 0) {
    parts.push(
      "",
      "Chinese text labels to draw by hand, reproduce them exactly and do not translate:",
      shot.labels.join(" / "),
    );
  }

  parts.push(
    "",
    "Constraints:",
    ...BASE_IMAGE_CONSTRAINTS,
    `Use at most ${style.maxLabels} short Chinese labels and write no text beyond the labels ` +
      "listed above. Do not put a title in any corner, do not name the physical situation on " +
      "the image, do not add a caption, signature or watermark.",
    style.negative.trim(),
  );

  return parts.join("\n");
}
