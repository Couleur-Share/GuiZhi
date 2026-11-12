// 线上对拍用桩：把 safe-fetch.ts 的目标校验换成「固定钉扎到 127.0.0.1」，请求只会发往本地捕获服务器。
export interface ResolvedAddress {
  address: string;
  family: 4 | 6;
}
export const isBlockedHostname = () => false;
export const isPrivateAddress = () => false;
export const resolvePublicAddress = async (): Promise<ResolvedAddress> => ({ address: "127.0.0.1", family: 4 });
