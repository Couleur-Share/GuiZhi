import { describe, expect, it } from "vitest";

import {
  buildContentSecurityPolicy,
  isInternalRendererUrl,
} from "../../../src/main/window-security";

const DEV_SERVER = "http://127.0.0.1:5173";
const RENDERER_DIR =
  process.platform === "win32" ? "C:\\app\\out\\renderer" : "/app/out/renderer";
const RENDERER_URL =
  process.platform === "win32"
    ? "file:///C:/app/out/renderer/index.html"
    : "file:///app/out/renderer/index.html";

describe("isInternalRendererUrl", () => {
  it("生产构建只允许渲染产物目录下的 file://", () => {
    expect(isInternalRendererUrl(RENDERER_URL, null, RENDERER_DIR)).toBe(true);
    expect(isInternalRendererUrl("https://example.com/", null, RENDERER_DIR)).toBe(
      false,
    );
    expect(isInternalRendererUrl(DEV_SERVER, null, RENDERER_DIR)).toBe(false);
  });

  it("渲染目录之外的本地文件同样拦截", () => {
    // preload 绑在 window 上，导航到任意本地 HTML 就等于交出 window.api；
    // 导入的资产会落在用户数据目录，那里必须是不可达的
    const outside =
      process.platform === "win32"
        ? "file:///C:/Users/someone/AppData/Roaming/GuiZhi/images/evil.html"
        : "file:///home/someone/.config/GuiZhi/images/evil.html";
    expect(isInternalRendererUrl(outside, null, RENDERER_DIR)).toBe(false);

    // 路径穿越也不行
    const traversal =
      process.platform === "win32"
        ? "file:///C:/app/out/renderer/../../evil.html"
        : "file:///app/out/renderer/../../evil.html";
    expect(isInternalRendererUrl(traversal, null, RENDERER_DIR)).toBe(false);
  });

  it("开发模式额外允许 dev server 同源", () => {
    expect(
      isInternalRendererUrl(`${DEV_SERVER}/index.html`, DEV_SERVER, RENDERER_DIR),
    ).toBe(true);
    // 同主机不同端口视为不同源
    expect(
      isInternalRendererUrl("http://127.0.0.1:9999/", DEV_SERVER, RENDERER_DIR),
    ).toBe(false);
    expect(
      isInternalRendererUrl("http://evil.example/", DEV_SERVER, RENDERER_DIR),
    ).toBe(false);
  });

  it("非法 URL 一律拦截", () => {
    expect(isInternalRendererUrl("", null, RENDERER_DIR)).toBe(false);
    expect(isInternalRendererUrl("not a url", null, RENDERER_DIR)).toBe(false);
    expect(
      isInternalRendererUrl("javascript:alert(1)", null, RENDERER_DIR),
    ).toBe(false);
  });
});

describe("buildContentSecurityPolicy", () => {
  it("生产不允许内联脚本与 eval", () => {
    const csp = buildContentSecurityPolicy(null);
    expect(csp).toContain("script-src 'self';");
    expect(csp).not.toContain("unsafe-eval");
    expect(csp).toContain("object-src 'none'");
    expect(csp).toContain("frame-src 'none'");
  });

  it("开发放开 HMR 需要的内联脚本与 websocket", () => {
    const csp = buildContentSecurityPolicy(DEV_SERVER);
    expect(csp).toContain("'unsafe-eval'");
    expect(csp).toContain("ws:");
  });

  it("本地媒体协议与外链图片保持可渲染", () => {
    const csp = buildContentSecurityPolicy(null);
    expect(csp).toContain("local-image:");
    expect(csp).toContain("local-video:");
    // 知识条目正文里的外链图片
    expect(csp).toMatch(/img-src[^;]*https:/);
  });
});
