import { describe, expect, it } from "vitest";
import {
  WEB_SUBFRAMES_ENV,
  shouldBlockSubframe,
} from "../../../src/main/services/web-capture/web-subframe-policy";

const TOP = "https://developer.chrome.com/docs/devtools/";

describe("子框架加载策略", () => {
  it("跨源子框架被拦截：视频嵌入、广告、统计", () => {
    expect(shouldBlockSubframe("https://www.youtube.com/embed/abc", TOP, {})).toBe(true);
    expect(shouldBlockSubframe("https://googleads.g.doubleclick.net/pagead/x", TOP, {})).toBe(true);
  });

  it("源由协议、主机、端口共同决定：同主机不同端口或协议也视为跨源", () => {
    expect(shouldBlockSubframe("https://developer.chrome.com:8443/x", TOP, {})).toBe(true);
    expect(shouldBlockSubframe("http://developer.chrome.com/x", TOP, {})).toBe(true);
    expect(shouldBlockSubframe("https://web.dev/x", TOP, {})).toBe(true);
  });

  it("同源子框架放行，与路径无关", () => {
    expect(shouldBlockSubframe("https://developer.chrome.com/widget", TOP, {})).toBe(false);
    expect(shouldBlockSubframe("https://developer.chrome.com/", TOP, {})).toBe(false);
  });

  it("非 http(s) 目标不产生网络请求，不拦截", () => {
    for (const url of ["about:blank", "blob:https://developer.chrome.com/uuid", "data:text/html,<p>x</p>", "javascript:void(0)"])
      expect(shouldBlockSubframe(url, TOP, {}), url).toBe(false);
  });

  it("无法判断时保持原行为：顶层地址为空、不是 http(s)，或子框架地址不合法", () => {
    const frame = "https://www.youtube.com/embed/abc";
    expect(shouldBlockSubframe(frame, "", {})).toBe(false);
    expect(shouldBlockSubframe(frame, "about:blank", {})).toBe(false);
    expect(shouldBlockSubframe("not a url", TOP, {})).toBe(false);
  });

  it(`${WEB_SUBFRAMES_ENV}=allow 恢复加载全部子框架；其他取值不改变默认`, () => {
    const frame = "https://www.youtube.com/embed/abc";
    expect(shouldBlockSubframe(frame, TOP, { [WEB_SUBFRAMES_ENV]: "allow" })).toBe(false);
    expect(shouldBlockSubframe(frame, TOP, { [WEB_SUBFRAMES_ENV]: "block" })).toBe(true);
    expect(shouldBlockSubframe(frame, TOP, { [WEB_SUBFRAMES_ENV]: "" })).toBe(true);
  });
});
