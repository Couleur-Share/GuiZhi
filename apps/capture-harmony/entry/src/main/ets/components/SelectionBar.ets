import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { pressFeedback, revealTransition } from '../services/InterfaceMotion';

// 多选操作栏：退出、已选数量、全选，以及批量发送与删除。多选期间页签栏收起，这里占据底部。
// 未连接电脑时“发送所选”变成“连接电脑”，与单条草稿的发送按钮一致。
@Component
export struct SelectionBar {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop count: number = 0;
  @Prop allSelected: boolean = false;
  @Prop connected: boolean = false;
  @Prop busy: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  onExit: () => void = () => {};
  onToggleAll: () => void = () => {};
  onSend: () => void = () => {};
  onDelete: () => void = () => {};
  onConnect: () => void = () => {};

  build() {
    Column({ space: 8 }) {
      Row({ space: 4 }) {
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.xmark')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).backgroundColor(Color.Transparent).clickEffect(pressFeedback(this.settings))
          .accessibilityText('退出多选').onClick(this.onExit)
        Text(this.count > 0 ? `已选 ${this.count} 条` : '请选择草稿').fontSize(15).fontWeight(FontWeight.Medium)
          .fontColor($r('app.color.text_primary')).layoutWeight(1).maxLines(1)
        Button(this.allSelected ? '取消全选' : '全选', { type: ButtonType.Normal }).constraintSize({ minHeight: 44 })
          .fontSize(14).padding({ left: 12, right: 12 }).borderRadius(12).backgroundColor(Color.Transparent)
          .fontColor(this.palette.accent).clickEffect(pressFeedback(this.settings)).onClick(this.onToggleAll)
      }.width('100%').alignItems(VerticalAlign.Center)
      Row({ space: 10 }) {
        Button(this.connected ? '发送所选' : '连接电脑', { type: ButtonType.Normal }).layoutWeight(1)
          .constraintSize({ minHeight: 48 }).fontSize(15).fontWeight(FontWeight.Medium).borderRadius(14)
          .backgroundColor(this.palette.soft).fontColor(this.palette.accent).clickEffect(pressFeedback(this.settings))
          .enabled(!this.busy && (!this.connected || this.count > 0))
          .onClick(() => { if (this.connected) this.onSend(); else this.onConnect(); })
        Button('删除所选', { type: ButtonType.Normal }).layoutWeight(1)
          .constraintSize({ minHeight: 48 }).fontSize(15).fontWeight(FontWeight.Medium).borderRadius(14)
          .backgroundColor($r('app.color.error_soft')).fontColor($r('app.color.error'))
          .clickEffect(pressFeedback(this.settings)).enabled(!this.busy && this.count > 0).onClick(this.onDelete)
      }.width('100%')
    }.width('100%').padding({ left: 12, right: 12, top: 8, bottom: 12 }).borderRadius(20)
      .backgroundColor($r('app.color.surface')).border({ width: 1, color: $r('app.color.border_subtle') })
      .transition(revealTransition(this.settings))
  }
}
