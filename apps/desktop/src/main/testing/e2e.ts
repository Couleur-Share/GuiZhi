import { app, ipcMain } from "electron";
import fs from "fs";
import path from "path";

import type Database from "../database/sqlite";

interface E2ESeedDocument {
  settings?: Record<string, unknown>;
}

interface E2EStats {
  webdav: {
    testConnection: number;
    ensureDirectory: number;
    upload: number;
    download: number;
    stat: number;
  };
}

const e2eStats: E2EStats = {
  webdav: {
    testConnection: 0,
    ensureDirectory: 0,
    upload: 0,
    download: 0,
    stat: 0,
  },
};

function createEmptyE2EStats(): E2EStats {
  return {
    webdav: {
      testConnection: 0,
      ensureDirectory: 0,
      upload: 0,
      download: 0,
      stat: 0,
    },
  };
}

export function resetE2EStats(): void {
  Object.assign(e2eStats, createEmptyE2EStats());
}

export function getE2EStats(): E2EStats {
  return JSON.parse(JSON.stringify(e2eStats)) as E2EStats;
}

export function getE2EWebDAVMode(
  env: NodeJS.ProcessEnv = process.env,
): "off" | "remote-empty" {
  return env.GUIZHI_E2E_WEBDAV_MODE === "remote-empty" ? "remote-empty" : "off";
}

export function registerE2EIPC(env: NodeJS.ProcessEnv = process.env): void {
  if (!isE2EEnabled(env)) {
    return;
  }

  ipcMain.handle("e2e:getStats", () => getE2EStats());
  ipcMain.handle("e2e:resetStats", () => {
    resetE2EStats();
    return true;
  });
}

export function handleE2EWebDAVRequest(
  action: keyof E2EStats["webdav"],
  fileUrl: string,
): Record<string, unknown> {
  e2eStats.webdav[action] += 1;

  const mode = getE2EWebDAVMode();
  if (mode !== "remote-empty") {
    return { success: false, error: "E2E WebDAV mock disabled" };
  }

  if (action === "stat") {
    return { success: false, notFound: true };
  }

  if (action === "download") {
    return { success: false, notFound: true };
  }

  if (action === "testConnection") {
    return { success: true, message: "E2E WebDAV mock connected" };
  }

  if (action === "ensureDirectory" || action === "upload") {
    return { success: true };
  }

  return {
    success: false,
    error: `Unhandled E2E WebDAV request for ${fileUrl}`,
  };
}

export function isE2EEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
  return env.GUIZHI_E2E === "1";
}

export function shouldUseDevServer(
  appIsPackaged: boolean,
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  if (isE2EEnabled(env)) {
    return false;
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
