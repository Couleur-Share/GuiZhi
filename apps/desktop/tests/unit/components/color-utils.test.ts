import { describe, expect, it } from "vitest";
import {
  hexToHsv,
  hsvToHex,
  normalizeHex,
  rgbToHsv,
} from "../../../src/renderer/components/ui/color-utils";

describe("normalizeHex", () => {
  it("三位简写补齐成六位，统一带 # 且小写", () => {
    expect(normalizeHex("#F0A")).toBe("#ff00aa");
    expect(normalizeHex("abc")).toBe("#aabbcc");
    expect(normalizeHex("  #3B82F6  ")).toBe("#3b82f6");
  });

  it("认不出的输入返回 null，不猜", () => {
    expect(normalizeHex("")).toBeNull();
    expect(normalizeHex("#12345")).toBeNull();
    expect(normalizeHex("rgb(0,0,0)")).toBeNull();
    expect(normalizeHex("#gggggg")).toBeNull();
  });
});

describe("hex 与 hsv 互转", () => {
  it("常见色 round-trip 不漂移", () => {
    for (const hex of [
      "#000000",
      "#ffffff",
      "#ff0000",
      "#00ff00",
      "#0000ff",
      "#3b82f6",
      "#7f7f7f",
    ]) {
      const hsv = hexToHsv(hex);
      expect(hsv).not.toBeNull();
      expect(hsvToHex(hsv!)).toBe(hex);
    }
  });

  it("色相落在各自扇区", () => {
    expect(Math.round(hexToHsv("#ff0000")!.h)).toBe(0);
    expect(Math.round(hexToHsv("#ffff00")!.h)).toBe(60);
    expect(Math.round(hexToHsv("#00ff00")!.h)).toBe(120);
    expect(Math.round(hexToHsv("#00ffff")!.h)).toBe(180);
    expect(Math.round(hexToHsv("#0000ff")!.h)).toBe(240);
    expect(Math.round(hexToHsv("#ff00ff")!.h)).toBe(300);
  });

  it("灰阶饱和度为 0，黑色明度为 0", () => {
    expect(rgbToHsv({ r: 128, g: 128, b: 128 }).s).toBe(0);
    expect(rgbToHsv({ r: 0, g: 0, b: 0 })).toEqual({ h: 0, s: 0, v: 0 });
  });

  it("色相越界先归一再换算，不产生非法颜色", () => {
    expect(hsvToHex({ h: 360, s: 1, v: 1 })).toBe("#ff0000");
    expect(hsvToHex({ h: -120, s: 1, v: 1 })).toBe("#0000ff");
    expect(hsvToHex({ h: 0, s: 5, v: 5 })).toBe("#ff0000");
  });

  it("无法解析的十六进制不进换算", () => {
    expect(hexToHsv("nope")).toBeNull();
  });
});
