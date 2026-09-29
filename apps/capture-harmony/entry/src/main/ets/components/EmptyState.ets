import { ThemePalette } from '../services/ThemePalette';

// 空状态：符号图标加标题与说明。图标只是装饰，读屏时只朗读标题与说明。
@Component
export struct EmptyState {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop icon: Resource = $r('sys.symbol.inbox');
  @Prop title: string = '';
  @Prop detail: string = '';

  build() {
    Column({ space: 10 }) {
      Column() {
        SymbolGlyph(this.icon).fontSize(34).fontColor([this.palette.accent]).accessibilityLevel('no')
      }.width(72).height(72).justifyContent(FlexAlign.Center).backgroundColor(this.palette.soft)
        .borderRadius(24).margin({ bottom: 6 })
      Text(this.title).fontSize(16).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
      Text(this.detail).fontSize(13).lineHeight(21).fontColor($r('app.color.text_secondary')).textAlign(TextAlign.Center)
    }.width('100%').padding({ top: 32, bottom: 32, left: 24, right: 24 })
  }
}
