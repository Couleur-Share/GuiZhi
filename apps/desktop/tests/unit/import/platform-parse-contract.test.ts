/**
 * 平台解析契约：最小真实形态 fixture。
 * 防回归——不声称能挡住线上改版；改坏 marker 必须得到 structure_missing。
 */
import fs from "fs";
import path from "path";
import { describe, expect, it, vi } from "vitest";
import { getPlatformParseCode } from "@guizhi/shared/utils/platform-parse-error";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: { getVersion: () => "0.0.0-test" },
}));

import { parseDouyinRouterData } from "../../../src/main/services/import/douyin";
import { parseXiaohongshuNote } from "../../../src/main/services/import/xiaohongshu";

const FIXTURES = path.resolve(__dirname, "../../fixtures/import");

function readFixture(name: string): string {
  return fs.readFileSync(path.join(FIXTURES, name), "utf8");
}

describe("平台解析契约夹具", () => {
  it("抖音最小 fixture：marker 与关键路径可解析", () => {
    const html = readFixture("douyin-router-data.min.html");
    expect(html).toContain("window._ROUTER_DATA");
    const aweme = parseDouyinRouterData(html, "7663897644049173802");
    expect(aweme.awemeId).toBe("7663897644049173802");
    expect(aweme.title).toContain("契约夹具");
    expect(aweme.playUrl).toContain("/play/");
  });

  it("抖音改掉 marker 名 → structure_missing", () => {
    const html = readFixture("douyin-router-data.min.html").replaceAll(
      "window._ROUTER_DATA",
      "window.__ROUTER_PAYLOAD__",
    );
    try {
      parseDouyinRouterData(html, "7663897644049173802");
      expect.unreachable("应抛错");
    } catch (error) {
      expect(getPlatformParseCode(error)).toBe("structure_missing");
    }
  });

  it("小红书最小 fixture：marker 与 noteDetailMap 可解析", () => {
    const html = readFixture("xiaohongshu-initial-state.min.html");
    expect(html).toContain("window.__INITIAL_STATE__");
    const note = parseXiaohongshuNote(html, "6a59e7f3000000000301fc49");
    expect(note.noteId).toBe("6a59e7f3000000000301fc49");
    expect(note.title).toBe("契约夹具");
    expect(note.imageMirrors.length).toBe(1);
  });

  it("小红书改掉 marker 名 → structure_missing", () => {
    const html = readFixture("xiaohongshu-initial-state.min.html").replaceAll(
      "window.__INITIAL_STATE__",
      "window.__APP_STATE__",
    );
    try {
      parseXiaohongshuNote(html, "6a59e7f3000000000301fc49");
      expect.unreachable("应抛错");
    } catch (error) {
      expect(getPlatformParseCode(error)).toBe("structure_missing");
    }
  });
});
