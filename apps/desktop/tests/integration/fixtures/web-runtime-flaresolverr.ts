import { DEFAULT_FLARESOLVERR_SETTINGS } from "@guizhi/shared/utils/flaresolverr";

// 本用例只验收普通离线采集；可选后备服务由独立单测覆盖。
export const getFlareSolverrSettings = () => DEFAULT_FLARESOLVERR_SETTINGS;
export async function captureWithFlareSolverr(): Promise<never> {
  throw new Error("离线采集夹具禁止使用 FlareSolverr");
}
