import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { SentEntry, SentLogLoad, SentLogPort, parseSent, serializeSent } from '../core/SentLog';

// 已发送记录的本机存储，与草稿分开放在独立的 Preferences 文件里：
// 它只是近期页的辅助信息，读写失败不能牵连草稿与发送。
// Preferences 在第一次读写时才打开，打开失败会在下一次读写时重试，而不是让应用启动失败。
export class SentLogStore implements SentLogPort {
  private context: common.Context;
  private opened: Promise<preferences.Preferences> | undefined;
  constructor(context: common.Context) { this.context = context; }

  private prefs(): Promise<preferences.Preferences> {
    if (!this.opened) {
      this.opened = preferences.getPreferences(this.context, { name: 'guizhi_capture_sent_v1' }).catch((error: Error) => {
        this.opened = undefined; throw error;
      });
    }
    return this.opened;
  }

  async load(): Promise<SentLogLoad> {
    const prefs = await this.prefs();
    return parseSent(await prefs.get('sent', '') as string);
  }

  async save(entries: SentEntry[]): Promise<void> {
    const prefs = await this.prefs();
    await prefs.put('sent', serializeSent(entries));
    await prefs.flush();
  }
}
