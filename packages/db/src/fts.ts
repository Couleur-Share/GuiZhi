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
 * 把用户搜索串构造成 FTS5 MATCH 查询。
 *
 * - 连续 CJK 片段 → 按字 phrase（"归 知"），保证字序相邻
 * - 非 CJK 词 → 前缀匹配（word*）
 * - 多个片段之间为 AND 关系
 *
 * 返回 null 表示查询串没有可检索内容。
 */
export function buildFtsMatchQuery(search: string): string | null {
  const trimmed = (search ?? "").trim();
  if (!trimmed) {
    return null;
  }

  const clauses: string[] = [];
  let cjkRun: string[] = [];
  let asciiBuffer = "";

  const flushCjk = () => {
    if (cjkRun.length > 0) {
      clauses.push(quoteFtsString(cjkRun.join(" ")));
      cjkRun = [];
    }
  };
  const flushAscii = () => {
    const word = asciiBuffer.trim();
    if (word) {
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

  return clauses.length > 0 ? clauses.join(" AND ") : null;
}
