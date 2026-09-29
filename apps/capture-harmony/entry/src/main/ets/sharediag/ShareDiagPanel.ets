// 分享入口 spike（仅 dev 包）：分享面板里的诊断摘要，由 ShareDiagExtAbility 通过 LocalStorage 提供数据。
import { common, UIExtensionContentSession } from '@kit.AbilityKit';
import { BusinessError, pasteboard } from '@kit.BasicServicesKit';

// useSharedStorage：使用扩展通过 session.loadContent(path, storage) 传入的 LocalStorage（LocalStorage.getShared() 已弃用）。
@Entry({ useSharedStorage: true })
@Component
struct ShareDiagPanel {
  @LocalStorageProp('lines') lines: string[] = [];
  @LocalStorageProp('report') report: string = '';
  @State hint: string = '';

  private async copyReport(): Promise<void> {
    try {
      await pasteboard.getSystemPasteboard().setData(pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, this.report));
      this.hint = '诊断报告已复制';
    } catch {
      this.hint = '复制失败，请打开完整报告页手动复制';
    }
  }

  // 从扩展里拉起主进程侧的诊断页：它会记录主进程 pid 基线，用来判断扩展是否独立进程。
  private async openReport(): Promise<void> {
    const context = this.getUIContext().getHostContext() as common.UIExtensionContext;
    try {
      await context.startAbility({ bundleName: context.applicationInfo.name, abilityName: 'ShareDiagAbility' });
    } catch (error) {
      this.hint = `无法打开完整报告页 错误码 ${(error as BusinessError).code}`;
    }
  }

  private close(): void {
    const session = this.getUIContext().getSharedLocalStorage()?.get<UIExtensionContentSession>('session');
    if (session) session.terminateSelf().catch(() => {});
  }

  build() {
    Column({ space: 12 }) {
      Text('归知收集 Dev · 分享诊断').fontSize(18).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
      Text('只记录字段长度与类型，不保存分享正文，也不会发送任何内容。')
        .fontSize(12).fontColor($r('app.color.text_secondary'))
      Scroll() {
        Column({ space: 6 }) {
          ForEach(this.lines, (line: string) => {
            Text(line).fontSize(13).fontColor($r('app.color.text_primary')).width('100%')
          }, (line: string, index: number) => `${index}:${line}`)
        }.alignItems(HorizontalAlign.Start).width('100%')
      }.layoutWeight(1).width('100%').scrollBar(BarState.Auto)
      if (this.hint) {
        Text(this.hint).fontSize(12).fontColor($r('app.color.accent')).width('100%')
      }
      Row({ space: 8 }) {
        Button('复制报告', { type: ButtonType.Normal })
          .layoutWeight(1).height(48).fontSize(14).enabled(this.report.length > 0)
          .backgroundColor($r('app.color.accent')).fontColor($r('app.color.on_accent'))
          .onClick(() => { this.copyReport(); })
        Button('完整报告页', { type: ButtonType.Normal })
          .layoutWeight(1).height(48).fontSize(14)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
          .onClick(() => { this.openReport(); })
        Button('关闭', { type: ButtonType.Normal })
          .layoutWeight(1).height(48).fontSize(14)
          .backgroundColor($r('app.color.surface_muted')).fontColor($r('app.color.text_primary'))
          .onClick(() => { this.close(); })
      }.width('100%')
    }
    .width('100%').height('100%').padding(20)
    .backgroundColor($r('app.color.page_background'))
  }
}
