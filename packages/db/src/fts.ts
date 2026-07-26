/**
 * FTS5 中文按字分词预处理。
 *
 * SQLite 内置 unicode61 tokenizer 不切分 CJK 文本（整句成为单个 token），
 * 中文检索无法命中。方案与归知 .NET 版（ADR 0005）一致：写入索引前在每个
 * CJK 字符两侧插入空格，使其成为独立 token；查询串做同样预处理，中文连续
 * 片段以 phrase（引号）匹配保持字序相邻。
 */

const CJK_PATTERN =
  /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\u3040-\u30ff\uac00-\ud7af]/;

function isCjkChar(char: string): boolean {
  return CJK_PATTERN.test(char);
}

/** 把文本预处理为「CJK 逐字分隔、其余词保持原样」的可索引形式。 */
export function segmentTextForFts(text: string): string {
  if (!text) {
    return "";
  }

  const parts: string[] = [];
  let asciiBuffer = "";

  const flushAscii = () => {
    for (const word of asciiBuffer.split(/\s+/)) {
      if (word) {
        parts.push(word);
      }
    }
    asciiBuffer = "";
  };

  for (const char of text) {
    if (isCjkChar(char)) {
      flushAscii();
      parts.push(char);
    } else {
      asciiBuffer += char;
    }
  }
  flushAscii();

  return parts.join(" ");
}

/** FTS5 字符串字面量转义（双引号翻倍）。 */
function quoteFtsString(value: string): string {
  return `"${value.replace(/"/g, '""')}"`;
}

/**
 * 片段是否含可被 unicode61 tokenizer 切出 token 的字符。
 *
 * 纯标点片段（"("、"："、"——"）分词后是空 phrase，空 phrase 匹配 0 行，
 * 再与其他子句 AND 会把整个查询清零——搜「归知(测试)」必然无结果。
 */
function hasSearchableToken(value: string): boolean {
  return /[\p{L}\p{N}]/u.test(value);
}

/**
 * 匹配模式。
 *
 * - `phrase`：片段之间 AND，中文整段按字相邻。用户在搜索框里敲什么就找什么。
 * - `recall`：先按虚词/疑问词切开长句，片段之间 OR，交给 bm25 排序。
 *   自然语言问句走 phrase 必然零命中——中文没有空格，整句会被编译成
 *   一个要求逐字连续出现的长 phrase。问答检索用这一档。
 */
export type FtsMatchMode = "phrase" | "recall";

/**
 * 中文虚词与疑问词：召回模式下在这些位置切开。
 * 「归知的语义检索是怎么实现的」→ 归知 / 语义检索 / 实现
 *
 * 按长度倒序排列，扫描时优先吃掉长词（「为什么」不该被「什么」先切掉一半）。
 */
const CJK_STOPWORDS = [
  "为什么", "是不是", "怎么样", "怎么办",
  "怎么", "怎样", "如何", "什么", "哪些", "哪个", "多少",
  "可以", "能否", "需要", "应该", "我们", "你们", "他们",
  "这个", "那个", "这些", "那些", "一下", "一个", "没有",
  "以及", "还是", "或者", "并且", "但是", "因为", "所以",
  "的", "了", "着", "是", "在", "和", "与", "及", "或",
  "吗", "呢", "吧", "啊", "有", "会", "要", "对", "把",
  "被", "从", "到", "给", "让", "用", "跟", "向", "并",
  "都", "也", "又", "很", "太", "就", "才", "再", "还",
].sort((left, right) => right.length - left.length);

/** OR 子句上限：再多只会把 bm25 的信噪比拖低 */
const MAX_RECALL_CLAUSES = 8;

/** 按虚词切开一段连续中文，返回实词片段 */
function splitCjkRunForRecall(run: string): string[] {
  const chars = [...run];
  const segments: string[] = [];
  let buffer: string[] = [];

  const flush = () => {
    if (buffer.length > 0) {
      segments.push(buffer.join(""));
      buffer = [];
    }
  };

  let index = 0;
  while (index < chars.length) {
    const rest = chars.slice(index).join("");
    const stopword = CJK_STOPWORDS.find((word) => rest.startsWith(word));
    if (stopword) {
      flush();
      index += [...stopword].length;
      continue;
    }
    buffer.push(chars[index]);
    index += 1;
  }
  flush();
  return segments;
}

/**
 * 把用户搜索串构造成 FTS5 MATCH 查询。
 *
 * - 连续 CJK 片段 → 按字 phrase（"归 知"），保证字序相邻
 * - 非 CJK 词 → 前缀匹配（word*）
 * - 片段之间的连接词由 mode 决定（见 FtsMatchMode）
 *
 * 返回 null 表示查询串没有可检索内容。
 */
export function buildFtsMatchQuery(
  search: string,
  mode: FtsMatchMode = "phrase",
): string | null {
  const trimmed = (search ?? "").trim();
  if (!trimmed) {
    return null;
  }

  const clauses: string[] = [];
  let cjkRun: string[] = [];
  let asciiBuffer = "";

  const pushCjkPhrase = (run: string) => {
    clauses.push(quoteFtsString([...run].join(" ")));
  };
  const flushCjk = () => {
    if (cjkRun.length === 0) {
      return;
    }
    const run = cjkRun.join("");
    cjkRun = [];
    if (mode === "phrase") {
      pushCjkPhrase(run);
      return;
    }
    const segments = splitCjkRunForRecall(run);
    // 单字片段（「的」剥完剩下的边角）几乎匹配全库，有实词时一律丢掉。
    // 整句都是虚词时退回原串——查得少好过返回 null 让上层当成「没搜索」
    const meaningful = segments.filter((segment) => [...segment].length > 1);
    const kept =
      meaningful.length > 0
        ? meaningful
        : segments.length > 0
          ? segments
          : [run];
    for (const segment of kept) {
      pushCjkPhrase(segment);
    }
  };
  const flushAscii = () => {
    const word = asciiBuffer.trim();
    if (word && hasSearchableToken(word)) {
      // 引号包裹后缀 * 实现安全的前缀匹配（token 内含特殊字符也不会破坏语法）
      clauses.push(`${quoteFtsString(word)}*`);
    }
    asciiBuffer = "";
  };

  for (const char of trimmed) {
    if (isCjkChar(char)) {
      flushAscii();
      cjkRun.push(char);
    } else if (/\s/.test(char)) {
      flushAscii();
      flushCjk();
    } else {
      flushCjk();
      asciiBuffer += char;
    }
  }
  flushAscii();
  flushCjk();

  if (clauses.length === 0) {
    return null;
  }
  return mode === "phrase"
    ? clauses.join(" AND ")
    : clauses.slice(0, MAX_RECALL_CLAUSES).join(" OR ");
}
