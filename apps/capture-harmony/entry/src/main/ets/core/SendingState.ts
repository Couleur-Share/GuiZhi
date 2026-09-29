// 页面上“正在发送”的草稿及其已等待时间。
// 只用不可变的纯函数：页面每次替换整个数组来触发界面刷新，逻辑可在 Node 中直接验证。
export class SendingEntry {
  requestId: string = '';
  startedAt: number = 0;
  seconds: number = 0;
}

// 与页面级操作提示保持一致：等待满 5 秒才提示网络较慢，避免正常发送时闪现。
export const SLOW_SEND_SECONDS: number = 5;

export function sendingFind(list: SendingEntry[], requestId: string): SendingEntry | undefined {
  for (let i = 0; i < list.length; i++) {
    if (list[i].requestId === requestId) return list[i];
  }
  return undefined;
}

// 登记一条开始发送；已在列表中则原样返回（同一条不会重复登记）。
export function sendingBegin(list: SendingEntry[], requestId: string, now: number): SendingEntry[] {
  if (sendingFind(list, requestId) !== undefined) return list;
  const entry = new SendingEntry();
  entry.requestId = requestId;
  entry.startedAt = now;
  return list.concat([entry]);
}

export function sendingEnd(list: SendingEntry[], requestId: string): SendingEntry[] {
  return list.filter((entry: SendingEntry): boolean => entry.requestId !== requestId);
}

// 依据当前时间重新计算每条的已等待秒数，返回新数组。
export function sendingTick(list: SendingEntry[], now: number): SendingEntry[] {
  return list.map((entry: SendingEntry): SendingEntry => {
    const next = new SendingEntry();
    next.requestId = entry.requestId;
    next.startedAt = entry.startedAt;
    next.seconds = Math.max(0, Math.floor((now - entry.startedAt) / 1000));
    return next;
  });
}

export function sendingLabel(seconds: number): string {
  return `发送中 · ${seconds} 秒`;
}

// 等待较久时的补充说明：强调可以继续记录，而不是要求用户等待。
export function sendingSlowHint(seconds: number): string {
  return seconds >= SLOW_SEND_SECONDS ? '网络较慢，可继续记录' : '';
}
