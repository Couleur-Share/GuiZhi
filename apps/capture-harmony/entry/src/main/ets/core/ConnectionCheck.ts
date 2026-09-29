// 仅重试读取状态，不自动重发配对或投递；限流期间手动检查也遵守冷却。
export class RelayRequestError extends Error {
  status: number;
  // 服务端错误码（如 daily_limit）或本地分类码（network / local_rate_limit）；未知时为空串。
  // 发送失败后据此判断“内容是否可能已被中转保存”，见 SendOutcome。
  code: string;
  constructor(message: string, status: number = 0, code: string = '') {
    super(message); this.status = status; this.code = code;
  }
}

export class ConnectionCheck {
  nextAttemptAt: number = 0;
  automatic: boolean = true;
  private failures: number = 0;
  private rateLimitedUntil: number = 0;

  canCheck(manual: boolean, now: number = Date.now()): boolean {
    if (now < this.rateLimitedUntil) return false;
    return manual || (this.automatic && now >= this.nextAttemptAt);
  }

  reset(): void {
    this.failures = 0; this.nextAttemptAt = 0; this.rateLimitedUntil = 0; this.automatic = true;
  }

  failed(error: Error, now: number = Date.now()): void {
    const status = error instanceof RelayRequestError ? error.status : 0;
    this.automatic = status === 0 || status === 408 || status === 429 || status >= 500;
    this.nextAttemptAt = now + Math.min(30000, 2000 * 2 ** Math.min(this.failures++, 4));
    if (status === 429) { this.rateLimitedUntil = now + 60000; this.nextAttemptAt = this.rateLimitedUntil; }
  }
}
