import { app } from "electron";
import fs from "fs";
import path from "path";

import { configureRuntimePaths as configureCoreRuntimePaths } from "@guizhi/core";

import type Database from "../database/sqlite";

interface E2ESeedDocument {
  settings?: Record<string, unknown>;
}

export function isE2EEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
  return env.GUIZHI_E2E === "1";
}

export function shouldUseDevServer(
  appIsPackaged: boolean,
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  if (isE2EEnabled(env)) {
    // Electron 33 + Playwright 在部分 Windows 图形环境里直接加载 file:// 构建
    // 产物会得到 ERR_FAILED。只在调用方显式给出隔离 renderer URL 时改走
    // loadURL()；普通 E2E 与正式启动路径保持不变。
    return Boolean(env.GUIZHI_E2E_RENDERER_URL);
  }
  return env.NODE_ENV === "development" || !appIsPackaged;
}

export function configureE2ETestProfile(
  env: NodeJS.ProcessEnv = process.env,
): string | null {
  if (!isE2EEnabled(env)) {
    return null;
  }

  const configuredDir = env.GUIZHI_E2E_USER_DATA_DIR;
  if (!configuredDir) {
    return null;
  }

  const resolvedDir = path.resolve(configuredDir);
  fs.mkdirSync(resolvedDir, { recursive: true });
  app.setName("GuiZhi E2E");
  app.setPath("userData", resolvedDir);
  // packages/core 有一份状态独立的同名路径解析，`config/` 下那三份 JSON
  // （ai-models / illustration-styles / mcp）全走它，而它认不得 app.setPath。
  // 不一起指过来的话，自动化实例会读写用户真实的 AI 配置——`pnpm shot` 截出来
  // 的是用户的模型列表，而任何落盘到设置的操作改的都是用户那份 Key。
  configureCoreRuntimePaths({ userDataPath: resolvedDir });
  return resolvedDir;
}

function readSeedDocument(
  env: NodeJS.ProcessEnv = process.env,
): E2ESeedDocument | null {
  const seedPath = env.GUIZHI_E2E_SEED_PATH;
  if (!seedPath) {
    return null;
  }

  const resolvedPath = path.resolve(seedPath);
  const raw = fs.readFileSync(resolvedPath, "utf8");
  return JSON.parse(raw) as E2ESeedDocument;
}

function writeSeedSettings(
  db: Database.Database,
  settings?: Record<string, unknown>,
): void {
  if (!settings || Object.keys(settings).length === 0) {
    return;
  }

  const stmt = db.prepare(
    "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
  );
  const transaction = db.transaction(() => {
    for (const [key, value] of Object.entries(settings)) {
      stmt.run(key, JSON.stringify(value));
    }
  });

  transaction();
}

/**
 * E2E 种子：目前支持写入设置项（settings 表键值）。
 */
export function applyE2ESeed(
  db: Database.Database,
  env: NodeJS.ProcessEnv = process.env,
): void {
  if (!isE2EEnabled(env)) {
    return;
  }

  const seed = readSeedDocument(env);
  if (!seed) {
    return;
  }

  writeSeedSettings(db, seed.settings);
}
