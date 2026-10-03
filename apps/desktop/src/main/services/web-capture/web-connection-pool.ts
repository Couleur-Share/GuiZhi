import type * as http from "node:http";
import { HttpProxyAgent } from "http-proxy-agent";
import { HttpsProxyAgent } from "https-proxy-agent";

/**
 * 一次网页采集内的代理连接复用范围。
 * Reusable proxy connections scoped to a single web capture.
 *
 * 背景：getHttpRequestAgent 返回的共享 agent 不带 keepAlive，系统代理模式还会为每个请求新建 agent，
 * 经代理的每个子请求都要重新建立隧道（实测新建约 0.7–0.9 s，复用约 0.2 s）。
 * 共享 agent 同时被更新器、图片下载、导入安全抓取等调用方使用，不能直接改成 keepAlive，
 * 因此由一次采集持有自己的 agent，采集结束立即销毁：连接寿命只有秒级，不跨采集共享。
 *
 * 只覆盖 HTTP(S) 代理；SOCKS 代理的 keepAlive 行为未验证，沿用原有逐请求行为。
 * 环境变量 `GUIZHI_WEB_CONNECTION_REUSE=0` 可关闭复用（排查某个站点或做 A/B 对照），
 * 关闭后 webNetworkRequest 的行为与引入本类之前完全一致。
 */
export const WEB_CONNECTION_REUSE_ENV = "GUIZHI_WEB_CONNECTION_REUSE";

/** 与网络出口槽位（WebTaskGate(8)）一致，不增加对单个源的并发压力。 */
const MAX_SOCKETS = 8;
const MAX_FREE_SOCKETS = 4;

export class WebConnectionPool {
  /** 构造时读取环境变量，一次采集内保持一致。 */
  readonly enabled = process.env[WEB_CONNECTION_REUSE_ENV] !== "0";
  private readonly agents = new Map<string, http.Agent>();
  private isClosed = false;

  get closed(): boolean {
    return this.isClosed;
  }

  /** 同一代理地址与目标协议共用一个 agent；https 目标走 CONNECT 隧道，http 目标走转发。 */
  agentFor(proxyUrl: string, secureTarget: boolean): http.Agent {
    if (this.isClosed) throw new Error("网页连接范围已关闭");
    const key = `${secureTarget ? "https" : "http"}|${proxyUrl}`;
    let agent = this.agents.get(key);
    if (!agent) {
      const options = {
        keepAlive: true,
        maxSockets: MAX_SOCKETS,
        maxFreeSockets: MAX_FREE_SOCKETS,
      };
      agent = (secureTarget
        ? new HttpsProxyAgent(proxyUrl, options)
        : new HttpProxyAgent(proxyUrl, options)) as unknown as http.Agent;
      this.agents.set(key, agent);
    }
    return agent;
  }

  /** 销毁全部连接；幂等。采集的静态与渲染阶段都结束后调用。 */
  close(): void {
    if (this.isClosed) return;
    this.isClosed = true;
    for (const agent of this.agents.values()) agent.destroy();
    this.agents.clear();
  }
}
