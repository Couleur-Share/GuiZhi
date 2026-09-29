import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { pressFeedback, revealTransition } from '../services/InterfaceMotion';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { Receipt, ProgressItem, receiptLabel, receiptTone } from '../core/Protocol';
import { relativeTime } from '../core/RelativeTime';

@Component
export struct ReceiptCard {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop receipt: Receipt;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop index: number = 0;
  onCopyId: () => void = () => {};

  // 状态文案的语义色：颜色只是辅助，文案本身已完整表达结果。
  private toneColor(): ResourceColor {
    const tone = receiptTone(this.receipt);
    if (tone === 'success') return $r('app.color.success');
    if (tone === 'warning') return $r('app.color.warning');
    if (tone === 'error') return $r('app.color.error');
    return $r('app.color.text_primary');
  }

  build() {
    Column({ space: 12 }) {
      Row({ space: 12 }) {
        Image($r('app.media.ic_history')).fillColor(this.palette.accent)
          .padding(10).width(40).height(40).backgroundColor(this.palette.soft).borderRadius(14)
        Text(`${this.receipt.itemCount} 项收集`).fontSize(16).fontWeight(FontWeight.Medium)
          .fontColor($r('app.color.text_primary')).layoutWeight(1)
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.more')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).clickEffect(pressFeedback(this.settings)).backgroundColor(Color.Transparent)
          .accessibilityText('记录操作')
          .bindMenu([{ value: '复制请求编号', action: this.onCopyId }], { systemMaterial: panelMaterial(this.settings) })
      }.width('100%')
      Text(receiptLabel(this.receipt)).fontSize(14).lineHeight(22).fontColor(this.toneColor()).width('100%')
      Text(relativeTime(this.receipt.createdAt, Date.now())).fontSize(12).fontColor($r('app.color.text_tertiary'))
      if (this.receipt.progress) {
        ForEach(this.receipt.progress.items.filter((item: ProgressItem) => item.error !== undefined), (item: ProgressItem) => {
          Text(`第 ${item.index + 1} 项：${item.error === 'login_required' ? '需在电脑登录来源网站' : item.error === 'incomplete' ? '内容不完整' : item.error === 'rate_limited' ? '来源限流' : item.error === 'network' ? '来源网络失败' : '采集失败'}`)
            .fontSize(13).fontColor($r('app.color.error')).width('100%')
        })
      }
    }.padding({ left: 18, right: 18, top: 12, bottom: 18 }).width('100%')
      .alignItems(HorizontalAlign.Start).backgroundColor($r('app.color.surface')).borderRadius(20)
      .transition(revealTransition(this.settings, this.index))
  }
}
