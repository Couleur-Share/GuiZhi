import { extractUrlsFromText, parseCaptureDraft, resolveCaptureAction } from "./capture-input";
import type { CaptureSubmission } from "../types/mobile-capture";

export class CaptureInputError extends Error {}
export function parseCaptureSubmission(value: unknown): { submission: CaptureSubmission; items: { kind: "text" | "url"; input: string }[] } {
  const v = value as CaptureSubmission | null;
  if (!v || typeof v.requestId !== "string" || !/^[a-zA-Z0-9_-]{16,128}$/.test(v.requestId) ||
      typeof v.input !== "string" || !["auto", "urls", "text"].includes(v.mode)) throw new CaptureInputError("invalid_input");
  if (new TextEncoder().encode(v.input).length > 32768) throw new CaptureInputError("input_too_large");
  if (!v.input.trim()) throw new CaptureInputError("empty_input");
  const draft = parseCaptureDraft(v.input);
  const action = v.mode === "text" ? { kind: "text" as const, text: v.input } :
    v.mode === "urls" ? { kind: "urls" as const, urls: extractUrlsFromText(v.input) } : resolveCaptureAction(draft, null);
  const items = action.kind === "urls" ? [...new Set(action.urls)].map(input => ({ kind: "url" as const, input })) :
    action.kind === "text" ? [{ kind: "text" as const, input: v.input }] : [];
  if (!items.length) throw new CaptureInputError("no_links");
  if (items.length > 20) throw new CaptureInputError("too_many_links");
  return { submission: { requestId: v.requestId, input: v.input, mode: v.mode }, items };
}
