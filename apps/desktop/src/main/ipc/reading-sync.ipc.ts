import { ipcMain } from 'electron';
import type Database from '@guizhi/db/adapter';
import { IPC_CHANNELS } from '@guizhi/shared/constants/ipc-channels';
import { ReadingController } from '../services/reading-sync/controller';
import { setReadingSyncStop } from '../services/reading-sync/lifecycle';

export function registerReadingSyncIPC(db: Database.Database) {
  const controller = new ReadingController(db);
  setReadingSyncStop(pause => controller.stop(pause));
  ipcMain.removeHandler(IPC_CHANNELS.READING_SYNC);
  ipcMain.handle(IPC_CHANNELS.READING_SYNC, async (_event, action: string, args: Record<string, unknown> = {}) => {
    try {
      if (process.env.GUIZHI_READING_SYNC_EXPERIMENTAL !== '1') {
        if (action === 'status') return { success: true, data: { available: false, configured: false, origin: null,
          collectionIds: [], sequence: 0, pending: false, busy: false, error: null } };
        throw new Error('手机阅读同步尚未开放');
      }
      if (action === 'status') return { success: true, data: controller.status() };
      const data = await controller.perform(async () => {
        switch (action) {
          case 'activate':
            if (typeof args.origin !== 'string' || typeof args.invite !== 'string') throw new Error('阅读同步激活参数无效');
            return controller.activate(args.origin, args.invite);
          case 'configure':
            if (!Array.isArray(args.collectionIds) || args.collectionIds.some(id => typeof id !== 'string')) throw new Error('知识库选择无效');
            return controller.configure(args.collectionIds as string[]);
          case 'sync': return controller.sync();
          case 'pair': return controller.pair();
          case 'pair-status': return controller.pairStatus();
          case 'approve':
            if (typeof args.deviceId !== 'string') throw new Error('阅读设备编号无效');
            return controller.approve(args.deviceId);
          case 'recovery': return controller.recovery();
          case 'revoke': return controller.revoke();
          default: throw new Error('不支持的阅读同步操作');
        }
      });
      return { success: true, data };
    } catch (error) { return { success: false, error: error instanceof Error ? error.message : '阅读同步失败，请重试' }; }
  });
}
