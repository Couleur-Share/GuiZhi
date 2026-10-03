import { describe, expect, it } from "vitest";
import {
  WEB_INERT_ENV,
  inertImageResponse,
  inertResourceMode,
  isInertImageRequest,
} from "../../../src/main/services/web-capture/web-inert-resources";

describe("渲染窗口的惰性图片", () => {
  it("默认以占位图本地应答；GUIZHI_WEB_INERT_RESOURCES=load 恢复真实加载", () => {
    expect(inertResourceMode({})).toBe("stub");
    expect(inertResourceMode({ [WEB_INERT_ENV]: "stub" })).toBe("stub");
    expect(inertResourceMode({ [WEB_INERT_ENV]: "load" })).toBe("load");
    // 无法识别的取值按默认处理，不会因拼写错误悄悄关闭。
    expect(inertResourceMode({ [WEB_INERT_ENV]: "off" })).toBe("stub");
  });

  it("只处理 Accept 属于图片类的 GET 请求", () => {
    expect(isInertImageRequest("GET", "image")).toBe(true);
    expect(isInertImageRequest("POST", "image")).toBe(false);
    expect(isInertImageRequest("HEAD", "image")).toBe(false);
    for (const kind of ["document", "style", "other"])
      expect(isInertImageRequest("GET", kind)).toBe(false);
  });

  it("应答是一张有效的 1×1 GIF，不缓存", async () => {
    const response = inertImageResponse(new Headers());
    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toBe("image/gif");
    expect(response.headers.get("cache-control")).toBe("no-store");
    const bytes = Buffer.from(await response.arrayBuffer());
    expect(bytes.subarray(0, 6).toString("ascii")).toBe("GIF89a");
    expect(bytes.readUInt16LE(6)).toBe(1);
    expect(bytes.readUInt16LE(8)).toBe(1);
    expect(bytes.at(-1)).toBe(0x3b);
    expect(response.headers.get("content-length")).toBe(String(bytes.length));
  });

  it("开启 COEP 的页面需要 CORP 头；带 Origin 的请求需要 CORS 头，否则页面会收到 onerror", () => {
    const plain = inertImageResponse(new Headers());
    expect(plain.headers.get("cross-origin-resource-policy")).toBe("cross-origin");
    expect(plain.headers.get("access-control-allow-origin")).toBeNull();

    const cors = inertImageResponse(new Headers({ origin: "https://example.com" }));
    expect(cors.headers.get("access-control-allow-origin")).toBe("https://example.com");
    expect(cors.headers.get("access-control-allow-credentials")).toBe("true");
    expect(cors.headers.get("vary")).toBe("Origin");
  });

  it("每次返回独立的应答体，一次被消费不影响下一次", async () => {
    const first = inertImageResponse(new Headers());
    const second = inertImageResponse(new Headers());
    await first.arrayBuffer();
    expect((await second.arrayBuffer()).byteLength).toBeGreaterThan(0);
  });
});
