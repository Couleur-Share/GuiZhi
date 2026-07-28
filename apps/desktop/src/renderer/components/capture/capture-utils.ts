import { detectForumPlatform } from "@guizhi/shared/utils/forum-platforms";
import { detectVideoPlatform } from "@guizhi/shared/utils/video-platforms";

/** 判断输入整体是否为单个 http(s) 链接（用于采集自动识别）。 */
export function isHttpUrlLike(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed || /\s/.test(trimmed) || !/^https?:\/\//i.test(trimmed)) {
    return false;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * 从正文里切链接的字符集：排除空白、成对包裹符与全角句读。
 *
 * 全角句读必须排除——小红书的口令是
 * `http://xhslink.com/a/xxx，复制本条信息…`，逗号紧贴着链接，只按空白切会把
 * 后半句一起吞进 URL。全角字母数字不排除，路径里出现中文是合法的。
 */
const URL_IN_TEXT_RE =
  /https?:\/\/[^\s<>"'`，。！？；：、（）【】《》「」『』…“”‘’]+/gi;

/** 半角句读跟在链接后面时属于句子而非 URL（`/` `#` 之类不在其中） */
const TRAILING_PUNCTUATION = ".,;:!?";

function countChar(value: string, char: string): number {
  let total = 0;
  for (const current of value) {
    if (current === char) {
      total += 1;
    }
  }
  return total;
}

/**
 * 修剪链接尾巴上的句读。括号按配平判断：维基百科那类
 * `/wiki/Foo_(bar)` 的右括号要留，`(见 https://x.com/a)` 里多出来的要去掉。
 */
function trimUrlTail(value: string): string {
  let url = value;
  while (url) {
    const last = url[url.length - 1];
    const unbalanced =
      (last === ")" && countChar(url, ")") > countChar(url, "(")) ||
      (last === "]" && countChar(url, "]") > countChar(url, "["));
    if (!TRAILING_PUNCTUATION.includes(last) && !unbalanced) {
      break;
    }
    url = url.slice(0, -1);
  }
  return url;
}

/** 从一段文本里按出现顺序抠出去重后的 http(s) 链接 */
export function extractUrlsFromText(value: string): string[] {
  const urls: string[] = [];
  const seen = new Set<string>();
  for (const match of value.match(URL_IN_TEXT_RE) ?? []) {
    const url = trimUrlTail(match);
    if (!isHttpUrlLike(url) || seen.has(url)) {
      continue;
    }
    seen.add(url);
    urls.push(url);
  }
  return urls;
}

/** 该链接有专用采集连接器（视频平台 / 论坛），而不是走通用网页抓取 */
function hasPlatformConnector(url: string): boolean {
  return detectVideoPlatform(url) !== null || detectForumPlatform(url) !== null;
}

export type CaptureDraft =
  | { kind: "empty" }
  | { kind: "urls"; urls: string[] }
  | { kind: "text"; text: string }
  /** 文字里夹着链接：两种解释都成立，由 prefer 定默认、界面上可改判 */
  | { kind: "mixed"; urls: string[]; text: string; prefer: "urls" | "text" };

/**
 * 解析采集框内容。
 *
 * 从浏览器书签或笔记里复制一批链接粘进来是采集的核心用例，而此前只要输入
 * 含任何空白就判定为非链接，整段被塞进一条文本笔记——没有报错也没有提示，
 * 用户以为导入了 20 个网页，实际得到一条纯文本。
 *
 * 按空白切分：全部是 http(s) 链接才作为链接批处理。混进文字的走 mixed——
 * 抖音「0.02 复制打开抖音，看看【…】 https://v.douyin.com/xxx/ :3pm 12/15」
 * 这类分享口令按整段存文本是纯粹的误判，那段话是平台生成的样板而非用户写的
 * 上下文；但「明天看这个 <链接>」里的说明文字是用户自己写的，丢掉才是误判。
 * 两者形状一样，区别只在链接指向哪儿：有专用连接器（视频平台 / 论坛）的
 * 默认采集链接，其余默认存文本，两种都能在提示栏上一键改判。
 */
export function parseCaptureDraft(value: string): CaptureDraft {
  const trimmed = value.trim();
  if (!trimmed) {
    return { kind: "empty" };
  }

  const tokens = trimmed.split(/\s+/).filter(Boolean);
  if (tokens.length > 0 && tokens.every(isHttpUrlLike)) {
    // 同一批里重复粘贴的链接去重，避免生成重复任务
    return { kind: "urls", urls: [...new Set(tokens)] };
  }

  const embedded = extractUrlsFromText(trimmed);
  if (embedded.length > 0) {
    return {
      kind: "mixed",
      urls: embedded,
      text: trimmed,
      prefer: embedded.every(hasPlatformConnector) ? "urls" : "text",
    };
  }

  return { kind: "text", text: trimmed };
}

export type CaptureAction =
  | { kind: "empty" }
  | { kind: "urls"; urls: string[] }
  | { kind: "text"; text: string };

/** 结合用户在提示栏上的改判，算出提交时真正要做的事 */
export function resolveCaptureAction(
  draft: CaptureDraft,
  override: "urls" | "text" | null,
): CaptureAction {
  if (draft.kind !== "mixed") {
    return draft;
  }
  return (override ?? draft.prefer) === "urls"
    ? { kind: "urls", urls: draft.urls }
    : { kind: "text", text: draft.text };
}
