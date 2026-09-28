import { ThemePalette } from '../services/ThemePalette';
import { animateInterface, revealTransition, pressFeedback } from '../services/InterfaceMotion';
import { AppPreferences } from '../services/AppPreferences';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { Draft } from '../core/Protocol';

@Component
export struct DraftCard {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop draft: Draft = new Draft();
  @Prop busy: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop index: number = 0;
  @Prop connected: boolean = false;
  @State expanded: boolean = false;
  onSend: () => void = () => {};
  onEdit: () => void = () => {};
  onRemove: () => void = () => {};
  onConnect: () => void = () => {};

  private source(): string {
    const match = /https?:\/\/([^\s/]+)/i.exec(this.draft.input);
    return match ? match[1] : '文字笔记';
  }

  build() {
    Column({ space: 12 }) {
      Row({ space: 10 }) {
        Image(this.draft.mode === 'text' || this.source() === '文字笔记' ? $r('app.media.ic_compose') : $r('app.media.ic_link'))
          .fillColor(this.palette.accent).padding(10).width(40).height(40).backgroundColor(this.palette.soft).borderRadius(14)
        Text(this.source()).fontSize(13).fontColor($r('app.color.text_secondary')).layoutWeight(1)
          .maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.chevron_down')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).clickEffect(pressFeedback(this.settings)).backgroundColor(Color.Transparent)
          .fontColor($r('app.color.text_secondary')).enabled(!this.busy).accessibilityText('草稿操作')
          .bindMenu(this.draft.attempted ? [
            { value: '删除本机草稿', action: this.onRemove }
          ] : [
            { value: '编辑内容', action: this.onEdit },
            { value: '删除本机草稿', action: this.onRemove }
          ], { systemMaterial: panelMaterial(this.settings) })
      }.width('100%')
      if (this.expanded) {
        Text(this.draft.input).fontSize(16).lineHeight(25).fontColor($r('app.color.text_primary'))
          .width('100%').copyOption(CopyOptions.LocalDevice)
          .transition(revealTransition(this.settings))
      } else {
        Text(this.draft.input).fontSize(16).lineHeight(25).fontColor($r('app.color.text_primary'))
          .width('100%').maxLines(this.settings.previewLines).textOverflow({ overflow: TextOverflow.Ellipsis })
          .copyOption(CopyOptions.LocalDevice).transition(revealTransition(this.settings))
      }
      Button(this.expanded ? '收起内容' : '展开内容').height(44).clickEffect(pressFeedback(this.settings)).fontSize(12)
        .backgroundColor(Color.Transparent).fontColor($r('app.color.text_secondary'))
        .onClick(() => { animateInterface(this.getUIContext(), this.settings, () => { this.expanded = !this.expanded; }); })
      Divider().color($r('app.color.border_subtle'))
      Row({ space: 12 }) {
        Column({ space: 4 }) {
          Text(this.draft.attempted ? '待确认结果' : '尚未发送').fontSize(12)
            .fontColor(this.draft.attempted ? $r('app.color.error') : $r('app.color.text_secondary'))
          Text(new Date(this.draft.createdAt).toLocaleString()).fontSize(11).fontColor($r('app.color.text_tertiary'))
        }.alignItems(HorizontalAlign.Start).layoutWeight(1)
        Button(this.busy ? '处理中' : !this.connected ? '连接电脑' : this.draft.attempted ? '重试发送' : '发送')
          .height(44).clickEffect(pressFeedback(this.settings)).fontSize(14).padding({ left: 18, right: 18 })
          .backgroundColor(this.palette.soft).fontColor(this.palette.accent)
          .enabled(!this.busy).onClick(() => { if (this.connected) this.onSend(); else this.onConnect(); })
      }.width('100%')
    }.width('100%').padding({ left: 18, right: 18, top: 12, bottom: 16 })
      .alignItems(HorizontalAlign.Start).backgroundColor($r('app.color.surface')).borderRadius(20)
      .transition(revealTransition(this.settings, this.index))
  }
}
