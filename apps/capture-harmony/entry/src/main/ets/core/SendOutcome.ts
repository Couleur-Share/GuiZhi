import { RelayRequestError } from './ConnectionCheck';

// 发送失败后，草稿的“已尝试发送”标记应如何处理：
// - rejected：可证明中转没有保存这条内容，可直接修改后重发；
// - notProcessed：请求没有被处理（鉴权/限流/协议在业务之前拦截），恢复到发送前的标记；
// - uncertain：可能已保存但回执丢失，必须保留标记，重试沿用原编号以保证幂等。
export type SendOutcome = 'rejected' | 'notProcessed' | 'uncertain';

// 配额类错误码（今日上限、收件箱已满、设备上限）：服务端明确拒绝，与请求频率无关。
export function isQuotaCode(code: string): boolean {
  return code === 'daily_limit' || code === 'inbox_full' || code === 'device_limit';
}

// 中转对 429 有两类：配额类发生在幂等查询之后，若此前已保存过同一编号就会直接返回旧回执，
// 因此出现即可证明未保存；频率类（rate_limited）与本地节流发生在一切业务之前，只能说明本次未处理。
export function classifySendFailure(error: Error): SendOutcome {
  if (!(error instanceof RelayRequestError)) return 'uncertain';
  const status = error.status;
  const code = error.code;
  // 400/413：内容校验在入库之前，服务端必然没有保存。
  if (status === 400 || status === 413) return 'rejected';
  if (status === 429) return isQuotaCode(code) ? 'rejected' : 'notProcessed';
  if (status === 401 || status === 403 || status === 426) return 'notProcessed';
  // 网络中断、超时、5xx、409 等：无法证明未保存。
  return 'uncertain';
}

// 写入草稿的失败原因：保留可复制的完整文案，限制长度避免异常响应撑大本机存储。
export function failureDetail(error: Error): string {
  const message = error.message ? error.message : '发送失败，请重试';
  return message.length > 300 ? message.substring(0, 300) : message;
}
