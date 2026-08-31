import path from "node:path";
import { BackupRepository } from "./backup-repository";
import { safeStorageKeyProtector } from "./safe-storage-key-protector";
import {
  getBackupsDir,
  getConfigDir,
  getDatabasePath,
  getImagesDir,
  getVideosDir,
} from "../runtime-paths";

let repository: BackupRepository | null = null;
let latestRendererSettings: Record<string, unknown> | undefined;

export function getBackupRepository(): BackupRepository {
  repository ??= new BackupRepository(
    {
      repositoryDir: path.join(getBackupsDir(), "repository"),
      databasePath: getDatabasePath(),
      configDir: getConfigDir(),
      imagesDir: getImagesDir(),
      videosDir: getVideosDir(),
    },
    safeStorageKeyProtector,
  );
  return repository;
}

export function setBackupRendererSettings(
  value: Record<string, unknown>,
): void {
  // IPC structured clone 会移除函数；再经 JSON 往返可拒绝 prototype 与不可序列化值。
  latestRendererSettings = JSON.parse(JSON.stringify(value)) as Record<
    string,
    unknown
  >;
}

export function getBackupRendererSettings():
  | Record<string, unknown>
  | undefined {
  return latestRendererSettings;
}

export function resetBackupRepositoryForTests(): void {
  repository = null;
  latestRendererSettings = undefined;
}
