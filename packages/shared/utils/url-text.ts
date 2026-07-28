/**
 * 从自由文本里抠 http(s) 链接。
 *
 * 两个调用方：快速采集框（把分享口令里的链接抠出来）与采集管线（把视频简介
 * 里的链接单独列进正文）。放 shared 而不是各写一份——那些尾部修剪规则是
 * 一条条实测踩出来的，分裂成两份迟早只修好其中一份。
 */

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
