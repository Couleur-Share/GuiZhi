import { UIAbility, Want, AbilityConstant, Configuration } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { systemShare } from '@kit.ShareKit';
import { util } from '@kit.ArkTS';
import { CaptureRuntime } from '../services/CaptureRuntime';
import { WindowAppearance } from '../services/WindowAppearance';
import { ShareRecordView, pickShareText, shareSavedNotice, shareFailureMessage } from '../core/ShareRecords';

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
    this.saveShare(want).catch((error: Error) => {
      AppStorage.setOrCreate('captureError', shareFailureMessage(error));
    });
  }

  private async saveShare(want: Want): Promise<void> {
    const data = await systemShare.getSharedData(want);
    const shared = data.getRecords();
    const records: ShareRecordView[] = [];
    for (let i = 0; i < shared.length; i++) {
      const record = shared[i];
      records.push(new ShareRecordView(record.utd, typeof record.content === 'string' ? record.content : ''));
    }
    const selection = pickShareText(records);
    const runtime = await CaptureRuntime.open(this.context);
    await runtime.queue.receive(util.generateRandomUUID(true), selection.text);
    AppStorage.setOrCreate('captureError', '');
    AppStorage.setOrCreate('captureRevision', (AppStorage.get<number>('captureRevision') ?? 0) + 1);
    AppStorage.setOrCreate('captureNotice', shareSavedNotice(selection.ignored));
  }
}
