import { ipcMain } from 'electron';
import Database from '../database/sqlite';
import { IPC_CHANNELS } from '@guizhi/shared/constants/ipc-channels';
import {
  changeMasterPassword,
  hasMasterPasswordConfigured,
  lock,
  securityStatus,
  setMasterPassword,
  unlock,
} from '../security';

export function registerSecurityIPC(db: Database.Database) {
  ipcMain.handle(IPC_CHANNELS.SECURITY_STATUS, async () => {
    return securityStatus(db);
  });

  ipcMain.handle(IPC_CHANNELS.SECURITY_SET_MASTER_PASSWORD, async (_e, password: string) => {
    if (!password || password.length < 4) {
      throw new Error('Password too short');
    }
    if (hasMasterPasswordConfigured(db)) {
      throw new Error('Master password is already configured');
    }
    setMasterPassword(db, password);
    return securityStatus(db);
  });

  ipcMain.handle(
    IPC_CHANNELS.SECURITY_CHANGE_MASTER_PASSWORD,
    async (_e, oldPassword: string, newPassword: string) => {
      if (!oldPassword) {
        throw new Error('Current password is required');
      }
      if (!newPassword || newPassword.length < 4) {
        throw new Error('Password too short');
      }
      if (!hasMasterPasswordConfigured(db)) {
        throw new Error('Master password is not configured');
      }

      const changed = changeMasterPassword(db, oldPassword, newPassword);
      if (!changed) {
        throw new Error('Current password is incorrect');
      }

      return securityStatus(db);
    },
  );

  ipcMain.handle(IPC_CHANNELS.SECURITY_UNLOCK, async (_e, password: string) => {
    const ok = unlock(db, password || '');
    return { success: ok, ...securityStatus(db) };
  });

  ipcMain.handle(IPC_CHANNELS.SECURITY_LOCK, async () => {
    lock();
    return securityStatus(db);
  });
}
