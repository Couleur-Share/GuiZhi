// 线上对拍用桩：无代理，与产品「未配置代理」时一致（getHttpRequestAgent 返回 undefined，走 Node 默认 agent）。
export const getHttpRequestAgent = () => undefined;
export const hasAnyProxyConfigured = () => false;
