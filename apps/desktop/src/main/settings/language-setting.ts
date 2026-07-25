import type { Language } from "@guizhi/shared/types";
import type Database from "../database/sqlite";

const SUPPORTED_LANGUAGES = new Set<Language>(["en", "zh"]);

export function readLanguageSetting(db: Database.Database): Language | null {
  try {
    const row = db
      .prepare("SELECT value FROM settings WHERE key = ?")
      .get("language") as { value: string } | undefined;
    if (!row) {
      return null;
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(row.value);
    } catch {
      parsed = row.value;
    }

    return typeof parsed === "string" &&
      SUPPORTED_LANGUAGES.has(parsed as Language)
      ? (parsed as Language)
      : null;
  } catch (error) {
    console.error("Failed to read language setting:", error);
    return null;
  }
}
