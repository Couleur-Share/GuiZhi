import { ThemePalette } from '../services/ThemePalette';

// 连接页的配对步骤：序号 + 标题 + 说明。
@Component
export struct PairingStep {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop step: string = '';
  @Prop title: string = '';
  @Prop detail: string = '';

  build() {
    Row({ space: 14 }) {
      Text(this.step).width(30).height(30).textAlign(TextAlign.Center).fontSize(13)
        .fontColor(this.palette.accent).backgroundColor(this.palette.soft).borderRadius(15)
      Column({ space: 5 }) {
        Text(this.title).fontSize(15).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
        Text(this.detail).fontSize(13).lineHeight(21).fontColor($r('app.color.text_secondary'))
      }.alignItems(HorizontalAlign.Start).layoutWeight(1)
    }.width('100%').alignItems(VerticalAlign.Top)
  }
}
