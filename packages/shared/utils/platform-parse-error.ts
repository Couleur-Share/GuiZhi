/**
 * 平台采集解析失败的稳定错误码。
 *
 * 任务 `error` 与日志用 `[code] 可读说明` 前缀，不改 schema 也能用正则抽出；
 * 改版类失败与「作品没了 / 缺 token」必须分开——前者要修解析器，后者用户自己能处理。
 */

export const PLATFORM_PARSE_ERROR_CODES = [
  "structure_missing",
  "note_unavailable",
  "token_invalid",
  "guest_denied",
  "network",
] as const;

export type PlatformParseErrorCode = (typeof PLATFORM_PARSE_ERROR_CODES)[number];

const CODE_SET = new Set<string>(PLATFORM_PARSE_ERROR_CODES);

const CODE_PREFIX_RE = /^\[([a-z_]+)\]\s*/;

export class PlatformParseError extends Error {
  readonly code: PlatformParseErrorCode;

  constructor(
    code: PlatformParseErrorCode,
    message: string,
    options?: { cause?: unknown },
  ) {
    const body = message.replace(CODE_PREFIX_RE, "").trim();
    super(`[${code}] ${body}`, options);
    this.name = "PlatformParseError";
    this.code = code;
  }
}

export function isPlatformParseErrorCode(
  value: string,
): value is PlatformParseErrorCode {
  return CODE_SET.has(value);
}

/** 从 Error / 带前缀的文案里抽出错误码 */
export function getPlatformParseCode(
  error: unknown,
): PlatformParseErrorCode | null {
  if (error instanceof PlatformParseError) {
    return error.code;
  }
  const message = error instanceof Error ? error.message : String(error);
  const match = CODE_PREFIX_RE.exec(message);
  if (!match || !isPlatformParseErrorCode(match[1])) {
    return null;
  }
  return match[1];
}
