import { UIAbility, Want, AbilityConstant, Configuration } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { systemShare } from '@kit.ShareKit';
import { util } from '@kit.ArkTS';
import { CaptureRuntime } from '../services/CaptureRuntime';
import { WindowAppearance } from '../services/WindowAppearance';

export default class EntryAbility extends UIAbility {
  private appearance: WindowAppearance = new WindowAppearance();
  onCreate(want: Want, _launchParam: AbilityConstant.LaunchParam): void { this.receive(want); }
  onNewWant(want: Want, _launchParam: AbilityConstant.LaunchParam): void { this.receive(want); }

  onForeground(): void { AppStorage.setOrCreate('captureForeground', true); }
  onBackground(): void { AppStorage.setOrCreate('captureForeground', false); }

  onWindowStageCreate(stage: window.WindowStage): void {
    this.openWindow(stage).catch(() => {
      AppStorage.setOrCreate('captureError', '窗口初始化失败，请退出后重新打开');
    });
  }

  private async openWindow(stage: window.WindowStage): Promise<void> {
    try { await this.appearance.attach(stage, this.context); }
    catch { AppStorage.setOrCreate('captureError', '系统栏初始化失败，请重新打开应用重试'); }
    stage.loadContent('pages/Index', (error) => {
      if (error.code) AppStorage.setOrCreate('captureError', '页面加载失败，请退出后重新打开');
    });
  }

  onConfigurationUpdate(_configuration: Configuration): void {
    this.appearance.updateColors().catch(() => {
      AppStorage.setOrCreate('captureError', '系统栏主题更新失败，请重新打开应用重试');
    });
  }

  onWindowStageDestroy(): void { this.appearance.detach(); }

  private receive(want: Want): void {
    if (want.action !== 'ohos.want.action.sendData') return;
    this.saveShare(want).catch(() => {
      AppStorage.setOrCreate('captureError', '本次分享未保存。请保留原内容，打开归知后重试，或复制文字到输入框。');
    });
  }

  private async saveShare(want: Want): Promise<void> {
    const data = await systemShare.getSharedData(want);
    const records = data.getRecords();
    const parts: string[] = [];
    for (let i = 0; i < records.length; i++) {
      const record = records[i];
      if (record.utd !== 'general.hyperlink' && record.utd !== 'general.plain-text' && record.utd !== 'general.text') {
        throw new Error('暂不支持图片或文件分享，请分享链接或文字');
      }
      if (typeof record.content !== 'string' || !record.content.trim()) throw new Error('分享缺少文字内容');
      parts.push(record.content); // 原样保存，不抽取或归一化 URL 参数。
    }
    if (parts.length === 0) throw new Error('分享内容为空');
    const runtime = await CaptureRuntime.open(this.context);
    await runtime.queue.receive(util.generateRandomUUID(true), parts.join('\n'));
    AppStorage.setOrCreate('captureError', '');
    AppStorage.setOrCreate('captureRevision', (AppStorage.get<number>('captureRevision') ?? 0) + 1);
    AppStorage.setOrCreate('captureNotice', '分享已保存到本机草稿，确认后发送');
  }
}
