import { ipcRenderer } from 'electron';
import { IPC_CHANNELS } from '@guizhi/shared/constants/ipc-channels';
import type { ReadingDesktopStatus } from '@guizhi/shared/types/reading-sync';

async function invoke<T>(action: string, args: Record<string, unknown> = {}): Promise<T> {
  const result = await ipcRenderer.invoke(IPC_CHANNELS.READING_SYNC, action, args);
  if (!result?.success) throw new Error(result?.error ?? '阅读同步失败');
  return result.data as T;
}
export const readingSyncApi = {
  status: () => invoke<ReadingDesktopStatus>('status'),
  activate: (origin: string, invite: string) => invoke<ReadingDesktopStatus>('activate', { origin, invite }),
  configure: (collectionIds: string[]) => invoke<ReadingDesktopStatus>('configure', { collectionIds }),
  sync: () => invoke<ReadingDesktopStatus>('sync'),
  pair: () => invoke<{ url: string; expiresAt: number }>('pair'),
  pairStatus: () => invoke<{ pairId: string; deviceId: string | null; approved: boolean } | null>('pair-status'),
  approve: (deviceId: string) => invoke<{ approved: boolean }>('approve', { deviceId }),
  recovery: () => invoke<{ recoveryKey: string; bundle: string; libraryId: string; generation: string; keyId: string }>('recovery'),
  revoke: () => invoke<{ revoked: boolean }>('revoke'),
};
