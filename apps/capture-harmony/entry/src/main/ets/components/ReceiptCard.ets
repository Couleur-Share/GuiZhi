import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { animateInterface, revealTransition, pressFeedback } from '../services/InterfaceMotion';
import { Receipt, ProgressItem, receiptLabel } from '../core/Protocol';

@Component
export struct ReceiptCard {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop receipt: Receipt;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop index: number = 0;
  @State expanded: boolean = false;

  build() {
    Column({ space: 12 }) {
      Row({ space: 12 }) {
        Image($r('app.media.ic_history')).fillColor(this.palette.accent)
          .padding(10).width(40).height(40).backgroundColor(this.palette.soft).borderRadius(14)
        Text(`${this.receipt.itemCount} 项收集`).fontSize(16).fontWeight(FontWeight.Medium)
          .fontColor($r('app.color.text_primary')).layoutWeight(1)
        Button(this.expanded ? '收起' : '详情').height(44).clickEffect(pressFeedback(this.settings)).fontSize(12).backgroundColor(Color.Transparent)
          .fontColor(this.palette.accent).onClick(() => { animateInterface(this.getUIContext(), this.settings, () => { this.expanded = !this.expanded; }); })
      }.width('100%')
      Text(receiptLabel(this.receipt)).fontSize(14).lineHeight(22).fontColor($r('app.color.text_primary')).width('100%')
      Text(new Date(this.receipt.createdAt).toLocaleString()).fontSize(12).fontColor($r('app.color.text_tertiary'))
      if (this.receipt.progress) {
        ForEach(this.receipt.progress.items.filter((item: ProgressItem) => item.error !== undefined), (item: ProgressItem) => {
          Text(`第 ${item.index + 1} 项：${item.error === 'login_required' ? '需在电脑登录来源网站' : item.error === 'incomplete' ? '内容不完整' : item.error === 'rate_limited' ? '来源限流' : item.error === 'network' ? '来源网络失败' : '采集失败'}`)
            .fontSize(13).fontColor($r('app.color.error')).width('100%')
        })
      }
      if (this.expanded) {
        Column({ space: 12 }) {
          Divider().color($r('app.color.border_subtle'))
          Text(`请求编号\n${this.receipt.requestId}`).fontSize(12).lineHeight(20)
            .fontColor($r('app.color.text_secondary')).copyOption(CopyOptions.LocalDevice).width('100%')
        }.width('100%').transition(revealTransition(this.settings))
      }
    }.padding({ left: 18, right: 18, top: 12, bottom: 18 }).width('100%')
      .alignItems(HorizontalAlign.Start).backgroundColor($r('app.color.surface')).borderRadius(20)
      .transition(revealTransition(this.settings, this.index))
  }
}
