/**
 * 阅读岛的消息协议（v2）：Go 宿主（apps/desktop-go/viva/reader）与岛内页面之间只传文本，内容是 JSON。
 * v2 相对 v1：复制带回执（copy 带 id，宿主回 copyResult）；编辑态（editor 视图：页面把文档的每次改动用 edit 事件交宿主，
 * 宿主用 focus 把焦点交给编辑器）。
 * v3 相对 v2：网页快照视图（snapshot 视图：整页 HTML 放进沙箱 iframe；页面用 snapshotState 上报目录是否常驻窄轨与开合；
 * 宿主用 ui 消息里的 snapshotWidth 增量改宽度；转发键增加 escape）。
 * v4 相对 v3：阅读区小数像素版面（宿主的 layout 消息：阅读区起点与尺寸带小数时，子窗口按 Viva 的规则取整到整数物理像素，
 * 宿主把「真实起点减去取整起点」与精确尺寸发来，页面把根节点挪过去并按精确尺寸设宽高，见 layout.ts；台账 B-068）。
 *
 * 方向与信任：
 * - 宿主 → 页面（HostMessage）：宿主是可信的，但页面仍按形状校验，坏消息直接丢弃，不能让 React 渲染崩掉。
 * - 页面 → 宿主（PageEvent）：宿主把它当作不可信输入校验（岛里可能渲染第三方内容，台账 A-018）；
 *   这里的类型只是页面自己的约定，Go 一侧另有一份校验（viva/reader/protocol.go），两侧字段名必须一致。
 *
 * Reader island protocol v1: JSON text between the Go host and the page.
 */

export const READER_PROTOCOL = 4;

export type ReaderLang = "zh" | "en";

/** 动效档位：与渲染层写到 <html data-motion> 上的取值一致（globals.css 据此缩放或关闭动画）。 */
export type ReaderMotion = "standard" | "reduced" | "off";

/** 外观输入：与渲染层 applyTheme 写到 documentElement 上的东西一一对应（dark 类、--theme-*、--base-font-size、data-motion）。 */
export interface ReaderTheme {
  dark: boolean;
  hue: number;
  saturation: number;
  /** 根字号，单位 px（14 / 16 / 18 三档，Tailwind 的 1rem）。 */
  basePx: number;
  /** 设置里的动效偏好与系统「减少动态效果」合并后的实际档位。 */
  motion: ReaderMotion;
  /** 窗口宽度（逻辑像素）：响应式断点按它判定，不按岛自己的宽度（breakpoints.ts）；缺省取页面自身宽度。 */
  viewportWidth?: number;
}

/**
 * 阅读区在小数像素上的版面（逻辑像素）：dx、dy 是真实起点减去子窗口取整起点（可为负，例如 222.75 - 223 = -0.25），
 * width、height 是真实的逻辑尺寸。落在设备像素上的阅读区没有版面（宿主发 null，或根本不发）。
 */
export interface ReaderLayout {
  dx: number;
  dy: number;
  width: number;
  height: number;
}

/** 内容面板的标签页（与渲染层 ReadingPanelTab 同一套取值）。 */
export type ReaderTab =
  | "body"
  | "transcript"
  | "images"
  | "recognized"
  | "summary"
  | "replies";

/**
 * 阅读区此刻要显示的东西。每种对应渲染层 ContentPanel 里 data-article-reader 内的一个分支：
 * - original：主题阅读的「原文」（ReconstructionReader 里的 MarkdownPreview，带目录浮层与标题位置跟踪）；
 * - markdown：纯 Markdown 预览（图片条目的识别文字标签等）；
 * - replies：论坛讨论（楼层卡片列表 + 楼层目录）；
 * - gallery：图片条目的配图画廊；
 * - editor：主题阅读「原文」的编辑态（ReconstructionReader 里的 MarkdownEditor，content 是草稿，文档由编辑器持有）；
 * - snapshot：公众号网页快照（WebSnapshotPane 的 original 分支：沙箱 iframe 的 srcDoc + 目录浮层）。
 */
export type ReaderView =
  | { kind: "none" }
  | { kind: "original"; content: string; sourceKind: "body" | "summary" }
  | { kind: "editor"; content: string; sourceKind: "body" | "summary" }
  | { kind: "markdown"; content: string; tab: ReaderTab }
  | { kind: "replies"; content: string }
  | { kind: "gallery"; content: string }
  | {
      kind: "snapshot";
      /** 整页 HTML（已净化，带 instanceId 标识与桥接脚本），只能放进沙箱 iframe 的 srcDoc。 */
      document: string;
      /** 这次读取的实例标识：iframe 的消息按它过滤，换版本或重新读取后旧消息作废。 */
      instanceId: string;
      /** 来源版本；空串表示当前版本（阅读位置按「条目 + 版本」记忆）。 */
      versionId: string;
      width: SnapshotWidth;
      /** 快照里的本地图片（local-image://…），点图时按它找序号交给图片查看岛。 */
      assets: string[];
    };

/** 网页快照的宽度档：original 固定 390 px，fluid 取舒适阅读宽度，wide 铺满。 */
export type SnapshotWidth = "original" | "fluid" | "wide";

export interface HostInit {
  t: "init";
  v: number;
  theme: ReaderTheme;
  lang: ReaderLang;
}

export interface HostView {
  t: "view";
  /** 宿主给每次换内容编的序号：页面上报的事件都带着它，宿主据此丢掉旧内容的迟到事件。 */
  seq: number;
  /** 条目 ID 与标签：页面按它们决定要不要重建（渲染层的 key={item.id}）。 */
  itemId: string;
  tab: ReaderTab;
  view: ReaderView;
  /** 打开这条时要恢复的滚动位置（逻辑像素）；没有记忆为 null。 */
  restoreTop: number | null;
  /** 楼层目录是否展开（论坛讨论标签）。 */
  catalogOpen: boolean;
  /** 原文的标题目录浮层是否展开。 */
  tocOpen: boolean;
}

export interface HostFind {
  t: "find";
  query: string;
  /** 当前命中序号（从 0 起，越界取模）。 */
  index: number;
}

export interface HostTheme {
  t: "theme";
  theme: ReaderTheme;
  lang: ReaderLang;
}

export interface HostUi {
  t: "ui";
  catalogOpen: boolean;
  tocOpen: boolean;
  /** 只在网页快照视图下带：宽度变化不重发整页文档（iframe 一重载，阅读位置与查找高亮全丢）。 */
  snapshotWidth?: SnapshotWidth;
}

/** 复制的回执：宿主写完系统剪贴板后告诉页面成败（对应 copy 事件的 id），页面据此让 writeText 成功或失败（台账 B-069）。 */
export interface HostCopyResult {
  t: "copyResult";
  id: number;
  ok: boolean;
}

/** 把键盘焦点交给编辑器（标题框里按 Enter：渲染层 ItemDetailHeader 的 `.cm-content.focus()`）；当前不是编辑视图时忽略。 */
export interface HostFocus {
  t: "focus";
}

/** 阅读区的小数像素版面；layout 为 null 表示阅读区落在设备像素上，页面回到默认的满视口。 */
export interface HostLayout {
  t: "layout";
  layout: ReaderLayout | null;
}

export type HostMessage =
  | HostInit
  | HostLayout
  | HostView
  | HostFind
  | HostTheme
  | HostUi
  | HostCopyResult
  | HostFocus;

export interface LightboxImageRef {
  src: string;
  alt?: string;
}

export type PageEvent =
  | { t: "ready"; v: number }
  | { t: "rendered"; seq: number }
  | { t: "matches"; seq: number; count: number }
  | { t: "scroll"; seq: number; top: number }
  | { t: "catalog"; seq: number; open: boolean }
  | { t: "toc"; seq: number; open: boolean }
  /** 网页快照的版面状态：目录是否因窗口够宽而常驻窄轨（原生工具栏据此决定要不要显示「目录」钮）与目录开合。 */
  | { t: "snapshotState"; seq: number; wideRail: boolean; open: boolean }
  | { t: "link"; seq: number; url: string }
  | { t: "copy"; seq: number; id: number; text: string }
  /** 编辑器里文档的当前全文（每次改动一条，不合并：宿主按它记草稿，「完成编辑」时用的就是最后一条）。 */
  | { t: "edit"; seq: number; text: string }
  | { t: "lightbox"; seq: number; images: LightboxImageRef[]; index: number }
  | { t: "toast"; seq: number; kind: "warning"; text: string }
  | {
      t: "key";
      seq: number;
      key: string;
      ctrl: boolean;
      alt: boolean;
      shift: boolean;
    }
  | { t: "findReset"; seq: number }
  | { t: "error"; seq: number; message: string };

const TABS: ReadonlySet<string> = new Set([
  "body",
  "transcript",
  "images",
  "recognized",
  "summary",
  "replies",
]);

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** 解析外观输入；形状不对（含根字号越界）返回 null。图片查看页的协议同样用它。 */
export function parseTheme(value: unknown): ReaderTheme | null {
  if (!isRecord(value)) return null;
  const { dark, hue, saturation, basePx, motion, viewportWidth } = value;
  if (
    typeof dark !== "boolean" ||
    typeof hue !== "number" ||
    typeof saturation !== "number" ||
    typeof basePx !== "number" ||
    (motion !== "standard" && motion !== "reduced" && motion !== "off")
  ) {
    return null;
  }
  if (![hue, saturation, basePx].every(Number.isFinite)) return null;
  // 根字号只认设置里的范围，防止坏值把整页字号弄成 0 或巨大。
  if (basePx < 8 || basePx > 40) return null;
  if (viewportWidth === undefined) return { dark, hue, saturation, basePx, motion };
  if (typeof viewportWidth !== "number" || !Number.isFinite(viewportWidth) || viewportWidth < 100 || viewportWidth > 20000) return null;
  return { dark, hue, saturation, basePx, motion, viewportWidth };
}

/** 版面的上限：与 Go 一侧 viva/reader/layout.go 的 maxLayoutOffset、maxLayoutSize 一致。 */
const MAX_LAYOUT_OFFSET = 1;
const MAX_LAYOUT_SIZE = 20000;

/** 解析小数像素版面；形状不对、偏移超出取整误差、尺寸非正或过大都返回 null（整条丢弃，页面保持原样）。 */
export function parseLayout(value: unknown): ReaderLayout | null {
  if (!isRecord(value)) return null;
  const { dx, dy, width, height } = value;
  if (
    typeof dx !== "number" ||
    typeof dy !== "number" ||
    typeof width !== "number" ||
    typeof height !== "number" ||
    ![dx, dy, width, height].every(Number.isFinite)
  ) {
    return null;
  }
  if (Math.abs(dx) > MAX_LAYOUT_OFFSET || Math.abs(dy) > MAX_LAYOUT_OFFSET) return null;
  if (width <= 0 || height <= 0 || width > MAX_LAYOUT_SIZE || height > MAX_LAYOUT_SIZE) return null;
  return { dx, dy, width, height };
}

export function parseLang(value: unknown): ReaderLang {
  return value === "en" ? "en" : "zh";
}

export function isSnapshotWidth(value: unknown): value is SnapshotWidth {
  return value === "original" || value === "fluid" || value === "wide";
}

function parseView(value: unknown): ReaderView | null {
  if (!isRecord(value)) return null;
  switch (value.kind) {
    case "none":
      return { kind: "none" };
    case "original":
    case "editor":
      if (
        typeof value.content === "string" &&
        (value.sourceKind === "body" || value.sourceKind === "summary")
      ) {
        return {
          kind: value.kind,
          content: value.content,
          sourceKind: value.sourceKind,
        };
      }
      return null;
    case "markdown":
      if (
        typeof value.content === "string" &&
        typeof value.tab === "string" &&
        TABS.has(value.tab)
      ) {
        return {
          kind: "markdown",
          content: value.content,
          tab: value.tab as ReaderTab,
        };
      }
      return null;
    case "replies":
    case "gallery":
      if (typeof value.content === "string") {
        return { kind: value.kind, content: value.content };
      }
      return null;
    case "snapshot":
      if (
        typeof value.document === "string" &&
        typeof value.instanceId === "string" &&
        value.instanceId !== "" &&
        typeof value.versionId === "string" &&
        isSnapshotWidth(value.width) &&
        Array.isArray(value.assets) &&
        value.assets.every((asset) => typeof asset === "string" && asset.startsWith("local-image://"))
      ) {
        return {
          kind: "snapshot",
          document: value.document,
          instanceId: value.instanceId,
          versionId: value.versionId,
          width: value.width,
          assets: value.assets as string[],
        };
      }
      return null;
    default:
      return null;
  }
}

/** 解析宿主发来的一条消息；形状不对返回 null。 */
export function parseHostMessage(text: string): HostMessage | null {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isRecord(raw)) return null;
  switch (raw.t) {
    case "init": {
      const theme = parseTheme(raw.theme);
      if (!theme || typeof raw.v !== "number") return null;
      return { t: "init", v: raw.v, theme, lang: parseLang(raw.lang) };
    }
    case "view": {
      const view = parseView(raw.view);
      if (
        !view ||
        typeof raw.seq !== "number" ||
        typeof raw.itemId !== "string" ||
        typeof raw.tab !== "string" ||
        !TABS.has(raw.tab)
      ) {
        return null;
      }
      const restoreTop =
        typeof raw.restoreTop === "number" && Number.isFinite(raw.restoreTop)
          ? Math.max(0, raw.restoreTop)
          : null;
      return {
        t: "view",
        seq: raw.seq,
        itemId: raw.itemId,
        tab: raw.tab as ReaderTab,
        view,
        restoreTop,
        catalogOpen: raw.catalogOpen === true,
        tocOpen: raw.tocOpen === true,
      };
    }
    case "find":
      if (typeof raw.query !== "string" || typeof raw.index !== "number") {
        return null;
      }
      return {
        t: "find",
        query: raw.query,
        index: Number.isFinite(raw.index) ? Math.trunc(raw.index) : 0,
      };
    case "theme": {
      const theme = parseTheme(raw.theme);
      if (!theme) return null;
      return { t: "theme", theme, lang: parseLang(raw.lang) };
    }
    case "ui":
      return {
        t: "ui",
        catalogOpen: raw.catalogOpen === true,
        tocOpen: raw.tocOpen === true,
        ...(isSnapshotWidth(raw.snapshotWidth) ? { snapshotWidth: raw.snapshotWidth } : {}),
      };
    case "copyResult":
      if (typeof raw.id !== "number" || !Number.isSafeInteger(raw.id) || typeof raw.ok !== "boolean") {
        return null;
      }
      return { t: "copyResult", id: raw.id, ok: raw.ok };
    case "focus":
      return { t: "focus" };
    case "layout": {
      if (raw.layout === null) return { t: "layout", layout: null };
      const layout = parseLayout(raw.layout);
      return layout ? { t: "layout", layout } : null;
    }
    default:
      return null;
  }
}
