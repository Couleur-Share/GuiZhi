import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { pressFeedback, revealTransition } from '../services/InterfaceMotion';

// 删除后的撤销条：草稿只是被隐藏，8 秒内点“撤销”即可原样恢复，超时才真正删除。
// 用页面内的普通组件而不是系统 Snackbar：它随页面状态出现和消失，撤销窗口结束时不会留下失效的按钮。
@Component
export struct UndoBar {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop message: string = '';
  @Prop settings: AppPreferences = new AppPreferences();
  onUndo: () => void = () => {};

  build() {
    Row({ space: 8 }) {
      Text(this.message).fontSize(14).lineHeight(20).fontColor($r('app.color.text_primary'))
        .layoutWeight(1).maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
      Button('撤销', { type: ButtonType.Normal }).constraintSize({ minHeight: 44 }).fontSize(14)
        .fontWeight(FontWeight.Medium).padding({ left: 14, right: 14 }).borderRadius(12)
        .backgroundColor(this.palette.soft).fontColor(this.palette.accent)
        .clickEffect(pressFeedback(this.settings)).onClick(this.onUndo)
    }.width('100%').padding({ left: 16, right: 8, top: 6, bottom: 6 }).borderRadius(16)
      .backgroundColor($r('app.color.surface')).border({ width: 1, color: $r('app.color.border_subtle') })
      .transition(revealTransition(this.settings))
  }
}
