import type Database from "../database/sqlite";

export function readBooleanSetting(
  db: Database.Database,
  key: string,
): boolean | null {
  try {
    const row = db.prepare("SELECT value FROM settings WHERE key = ?").get(key) as
      | { value: string }
      | undefined;
    if (!row) {
      return null;
    }
    return JSON.parse(row.value) === true;
  } catch (error) {
    console.error(`Failed to read ${key} setting:`, error);
    return null;
  }
}

export function getMinimizeOnLaunchSetting(db: Database.Database): boolean {
  return readBooleanSetting(db, "minimizeOnLaunch") ?? false;
}

export function readGithubTokenSetting(db: Database.Database): string | null {
  try {
    const row = db.prepare("SELECT value FROM settings WHERE key = ?").get(
      "githubToken",
    ) as { value: string } | undefined;
    if (!row) {
      return null;
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(row.value);
    } catch {
      parsed = row.value;
    }

    if (typeof parsed !== "string") {
      return null;
    }

    if (/[\r\n\x00-\x1f\x7f]/.test(parsed)) {
      return null;
    }

    const trimmed = parsed.trim();
    return trimmed.length > 0 ? trimmed : null;
  } catch (error) {
    console.error("Failed to read githubToken setting:", error);
    return null;
  }
}
