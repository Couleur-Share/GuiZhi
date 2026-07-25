/**
 * AI 响应错误的可读化。
 *
 * provider 与中转站的错误体形态各异：结构化 JSON、纯文本、网关返回的
 * HTML 错误页都可能出现。这里统一收敛成一句人能看懂的话。
 */
import type { ResponseLike } from "./ai-types";

function getResponseHeader(
  headers: Record<string, string>,
  name: string,
): string {
  const lowerName = name.toLowerCase();
  const match = Object.entries(headers).find(
    ([key]) => key.toLowerCase() === lowerName,
  );
  return match?.[1] ?? "";
}

function isHtmlErrorPayload(
  text: string,
  headers: Record<string, string>,
): boolean {
  const contentType = getResponseHeader(headers, "content-type").toLowerCase();
  const trimmed = text.trimStart().toLowerCase();
  return (
    contentType.includes("text/html") ||
    trimmed.startsWith("<!doctype html") ||
    trimmed.startsWith("<html")
  );
}

function extractHtmlTitle(text: string): string | null {
  const match = text.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
  return match?.[1]?.replace(/\s+/g, " ").trim() || null;
}

function formatGatewayTimeoutMessage(
  operation: string,
  status: number,
): string {
  return `${operation} gateway timed out (${status}). The provider or proxy did not finish before its own timeout.`;
}

export function parseStructuredErrorMessage(text: string): string | null {
  try {
    const errorJson = JSON.parse(text);
    const message =
      errorJson.error?.message ||
      errorJson.error?.status ||
      errorJson.error?.type ||
      errorJson.message ||
      errorJson.detail ||
      (typeof errorJson.error === "string" ? errorJson.error : null);

    if (!message) {
      return null;
    }

    if (errorJson.error?.code) {
      return `${message} (code: ${errorJson.error.code})`;
    }
    if (errorJson.error?.type && errorJson.error.type !== message) {
      return `[${errorJson.error.type}] ${message}`;
    }
    return message;
  } catch {
    return null;
  }
}

export async function getFormattedErrorMessageFromResponse(
  response: ResponseLike,
  options: {
    operation?: string;
    fallback?: string;
    maxTextLength?: number;
  } = {},
): Promise<string> {
  const errorText = response.error ?? (await response.text());
  const operation = options.operation ?? "API request";
  const fallback = options.fallback ?? `API 请求失败 (${response.status})`;

  if (response.status === 504) {
    return formatGatewayTimeoutMessage(operation, response.status);
  }

  const structuredMessage = parseStructuredErrorMessage(errorText);
  if (structuredMessage) {
    return structuredMessage;
  }

  if (errorText && isHtmlErrorPayload(errorText, response.headers)) {
    const title = extractHtmlTitle(errorText);
    return title ? `${fallback}: ${title}` : fallback;
  }

  if (errorText) {
    return errorText.slice(0, options.maxTextLength ?? 200);
  }

  return fallback;
}
