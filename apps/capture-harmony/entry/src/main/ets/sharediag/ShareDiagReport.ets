// 分享入口 spike（仅 dev 包）：诊断报告页。打开时先记录一条主进程基线（含同样的 Asset Store / 凭证 / 网络探测），
// 再展示完整报告；“复制诊断报告”得到的文本只含长度与类别，可以直接发给开发者。
import { common } from '@kit.AbilityKit';
import { pasteboard } from '@kit.BasicServicesKit';
import { process } from '@kit.ArkTS';
import { ShareDiagStore } from './ShareDiagStore';
import { newMainEntry } from './ShareProbe';
import { runProbes } from './ShareRuntimeProbe';

@Entry
@Component
struct ShareDiagReport {
  @State report: string = '正在生成报告…';
  @State notice: string = '';
  @State busy: boolean = true;
  private store: ShareDiagStore | undefined = undefined;

  aboutToAppear(): void {
    this.store = new ShareDiagStore(this.getUIContext().getHostContext() as common.Context);
    this.refresh(true).catch((error: Error) => { this.fail(error); });
  }

  private fail(error: Error): void {
    this.busy = false;
    this.notice = `读取诊断记录失败：${error.message}`;
  }

  // probe 为 true 时先重新记录主进程基线（会真实访问一次中转的 session 接口，不发送内容）。
  private async refresh(probe: boolean): Promise<void> {
    const store = this.store;
    if (store === undefined) return;
    this.busy = true;
    if (probe) {
      const entry = newMainEntry(Date.now(), process.pid);
      store.upsert(entry);
      this.report = store.report();
      await runProbes(entry, () => { store.upsert(entry); });
    }
    this.report = store.report();
    this.busy = false;
  }

  private async copyReport(): Promise<void> {
    try {
      await pasteboard.getSystemPasteboard().setData(pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, this.report));
      this.notice = '诊断报告已复制';
    } catch {
      this.notice = '复制失败，请长按报告文字手动复制';
    }
  }

  private clearAll(): void {
    try {
      this.store?.clear();
      this.notice = '记录已清空';
    } catch (error) {
      this.notice = `清空失败：${(error as Error).message}`;
    }
    this.refresh(true).catch((error: Error) => { this.fail(error); });
  }

  build() {
    Column({ space: 12 }) {
      Text('分享诊断（Dev）').fontSize(20).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
      Text('先用本页确认基线，再从浏览器、微信、小红书、抖音各分享一次到「归知收集 Dev」，回来刷新查看。')
        .fontSize(12).fontColor($r('app.color.text_secondary')).width('100%')
      if (this.notice) {
        Text(this.notice).fontSize(12).fontColor($r('app.color.accent')).width('100%')
      }
      Scroll() {
        Text(this.report).fontSize(12).fontColor($r('app.color.text_primary')).width('100%').copyOption(CopyOptions.LocalDevice)
      }.layoutWeight(1).width('100%').scrollBar(BarState.Auto)
      Row({ space: 8 }) {
        Button('复制诊断报告', { type: ButtonType.Normal }).layoutWeight(2).height(48).enabled(!this.busy)
          .backgroundColor($r('app.color.accent')).fontColor($r('app.color.on_accent'))
          .onClick(() => { this.copyReport(); })
        Button('刷新', { type: ButtonType.Normal }).layoutWeight(1).height(48).enabled(!this.busy)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
          .onClick(() => { this.refresh(true).catch((error: Error) => { this.fail(error); }); })
        Button('清空', { type: ButtonType.Normal }).layoutWeight(1).height(48).enabled(!this.busy)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.error'))
          .onClick(() => { this.clearAll(); })
      }.width('100%')
    }
    .width('100%').height('100%').padding(20)
    .backgroundColor($r('app.color.page_background'))
  }
}
