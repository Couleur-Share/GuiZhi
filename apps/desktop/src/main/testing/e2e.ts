import { app } from "electron";
import fs from "fs";
import path from "path";

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
