import { safeStorage } from "electron";
import type { RepositoryKeyProtector } from "./backup-repository-crypto";

function selectedBackend(): string {
  try {
    return safeStorage.getSelectedStorageBackend();
  } catch {
    return "unavailable";
  }
}

/** Electron safeStorage 适配；Linux basic_text 明确视为不安全，禁止无人值守解锁。 */
export const safeStorageKeyProtector: RepositoryKeyProtector = {
  get backend() {
    return selectedBackend();
  },
  isAvailable: () => safeStorage.isEncryptionAvailable(),
  isSecure: () =>
    safeStorage.isEncryptionAvailable() && selectedBackend() !== "basic_text",
  wrap: (key) => safeStorage.encryptString(key.toString("base64")),
  unwrap: (wrapped) =>
    Buffer.from(safeStorage.decryptString(wrapped), "base64"),
};
