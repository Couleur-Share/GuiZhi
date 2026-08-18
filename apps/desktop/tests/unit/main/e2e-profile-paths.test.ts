import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getConfigDir as getCoreConfigDir,
  resetRuntimePaths as resetCoreRuntimePaths,
} from "@guizhi/core";

// vi.mock 的工厂被提升到文件顶部，引用的变量必须一并提升
const { appMock } = vi.hoisted(() => ({
  appMock: { setName: () => {}, setPath: () => {} },
}));
vi.mock("electron", () => ({ app: appMock, session: { defaultSession: {} } }));

import {
  configureE2ETestProfile,
  shouldUseDevServer,
} from "../../../src/main/testing/e2e";

/**
 * `config/` 下那三份 JSON（ai-models / illustration-styles / mcp）走的是
 * packages/core 那份路径解析，它认不得 app.setPath。漏掉这一步的表现是
 * 自动化实例读写用户真实的 AI 配置：`pnpm shot` 截出来的是用户的模型列表，
 * 而任何落盘到设置的操作改的都是用户那份 Key。
 */
const TEST_USER_DATA = path.join(os.tmpdir(), "guizhi-e2e-profile-test");

afterEach(() => {
  resetCoreRuntimePaths();
  fs.rmSync(TEST_USER_DATA, { recursive: true, force: true });
});

describe("E2E 测试 profile 的路径隔离", () => {
  it("core 的 config 目录跟着临时数据目录走", () => {
    const realConfigDir = getCoreConfigDir();

    const resolved = configureE2ETestProfile({
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: TEST_USER_DATA,
    });

    expect(resolved).toBe(path.resolve(TEST_USER_DATA));
    expect(getCoreConfigDir()).toBe(
      path.join(path.resolve(TEST_USER_DATA), "config"),
    );
    expect(getCoreConfigDir()).not.toBe(realConfigDir);
  });

  it("非 E2E 启动不碰 core 的路径", () => {
    const realConfigDir = getCoreConfigDir();

    expect(configureE2ETestProfile({})).toBeNull();

    expect(getCoreConfigDir()).toBe(realConfigDir);
  });
});

describe("E2E renderer 加载策略", () => {
  it("默认仍走生产 loadFile 路径", () => {
    expect(shouldUseDevServer(false, { GUIZHI_E2E: "1" })).toBe(false);
  });

  it("只有显式给出隔离 renderer URL 才允许改走 loadURL", () => {
    expect(
      shouldUseDevServer(false, {
        GUIZHI_E2E: "1",
        GUIZHI_E2E_RENDERER_URL: "http://127.0.0.1:4173",
      }),
    ).toBe(true);
  });
});
