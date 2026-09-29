import { DesktopStatus } from '../core/DesktopStatus';
import { ReceiptTone } from '../core/Protocol';

// 电脑在线状态行：收集页标题下方左对齐，连接页状态卡居中。
// 语义色只是辅助：状态行的文字本身已完整表达状态，不依赖颜色区分。
@Component
export struct DesktopStatusLine {
  @Prop desktop: DesktopStatus = new DesktopStatus();
  @Prop center: boolean = false;

  private toneColor(tone: ReceiptTone): ResourceColor {
    if (tone === 'success') return $r('app.color.success');
    if (tone === 'warning') return $r('app.color.warning');
    if (tone === 'error') return $r('app.color.error');
    return $r('app.color.text_secondary');
  }

  build() {
    Row({ space: 8 }) {
      Row().width(8).height(8).borderRadius(4).backgroundColor(this.toneColor(this.desktop.tone))
      Text(this.desktop.summary).fontSize(13).lineHeight(20).fontColor($r('app.color.text_secondary'))
        .textAlign(this.center ? TextAlign.Center : TextAlign.Start).constraintSize({ maxWidth: '85%' })
        .maxLines(3).textOverflow({ overflow: TextOverflow.Ellipsis })
    }.width('100%').alignItems(VerticalAlign.Center).justifyContent(this.center ? FlexAlign.Center : FlexAlign.Start)
      .accessibilityGroup(true).accessibilityText(this.desktop.summary)
  }
}
