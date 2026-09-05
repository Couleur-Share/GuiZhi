import type { PlatformCaptureErrorCode } from "@guizhi/shared/types";

export class PlatformCaptureError extends Error {
  constructor(readonly code: PlatformCaptureErrorCode, message: string, options?: { cause?: unknown }) {
    super(`[${code}] ${message}`, options);
    this.name = "PlatformCaptureError";
  }
}
