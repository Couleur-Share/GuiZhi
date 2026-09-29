import { LengthMetrics } from '@kit.ArkUI';
import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { pressFeedback, revealTransition } from '../services/InterfaceMotion';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { ReceiptBadge, ReceiptTone, receiptBadges, receiptStatus, receiptTone } from '../core/Protocol';
import { FailedItem, HistoryItem, failedItems } from '../core/HistoryView';
import { relativeTime } from '../core/RelativeTime';

@Component
export struct ReceiptCard {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop item: HistoryItem;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop index: number = 0;
  onCopyId: () => void = () => {};
  onCopyUrl: (url: string) => void = (url: string): void => {};
  onOpenUrl: (url: string) => void = (url: string): void => {};
  onCopyReason: (summary: string) => void = (summary: string): void => {};

  // 语义色只是辅助：状态与计数的文字本身已完整表达结果，不依赖颜色区分。
  private toneColor(tone: ReceiptTone): ResourceColor {
    if (tone === 'success') return $r('app.color.success');
    if (tone === 'warning') return $r('app.color.warning');
    if (tone === 'error') return $r('app.color.error');
    return $r('app.color.text_secondary');
  }

  private toneBackground(tone: ReceiptTone): ResourceColor {
    if (tone === 'success') return $r('app.color.success_soft');
    if (tone === 'warning') return $r('app.color.warning_soft');
    if (tone === 'error') return $r('app.color.error_soft');
    return $r('app.color.surface_muted');
  }

  // 失败项的操作：有对应链接时可复制或在浏览器打开；原因总是可以复制，便于粘贴给排查的人。
  private failureMenu(failed: FailedItem): MenuElement[] {
    const menu: MenuElement[] = [];
    if (failed.url !== '') {
      menu.push({ value: '复制链接', action: () => { this.onCopyUrl(failed.url); } });
      menu.push({ value: '在浏览器打开', action: () => { this.onOpenUrl(failed.url); } });
    }
    menu.push({ value: '复制原因', action: () => { this.onCopyReason(failed.summary); } });
    return menu;
  }

  @Builder
  private Pill(text: string, foreground: ResourceColor, background: ResourceColor) {
    Text(text).fontSize(12).fontColor(foreground).backgroundColor(background).borderRadius(8)
      .padding({ left: 8, right: 8, top: 3, bottom: 3 }).maxLines(1)
  }

  @Builder
  private FailedRow(failed: FailedItem) {
    Row({ space: 8 }) {
      Text(failed.summary).fontSize(13).lineHeight(20).fontColor($r('app.color.error')).layoutWeight(1)
        .maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
      SymbolGlyph($r('sys.symbol.more')).fontSize(18).fontColor([$r('app.color.text_secondary')])
    }.width('100%').constraintSize({ minHeight: 44 }).padding({ left: 12, right: 8 })
      .backgroundColor($r('app.color.surface_muted')).borderRadius(12)
      .accessibilityDescription('点击打开操作菜单')
      .bindMenu(this.failureMenu(failed), { systemMaterial: panelMaterial(this.settings) })
  }

  build() {
    Column({ space: 10 }) {
      Row({ space: 8 }) {
        if (this.item.source !== '') {
          this.Pill(this.item.source, this.palette.accent, this.palette.soft)
        }
        Text(relativeTime(this.item.receipt.createdAt, Date.now())).fontSize(12)
          .fontColor($r('app.color.text_tertiary')).layoutWeight(1)
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.more')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).clickEffect(pressFeedback(this.settings)).backgroundColor(Color.Transparent)
          .accessibilityText('记录操作')
          .bindMenu([{ value: '复制请求编号', action: this.onCopyId }], { systemMaterial: panelMaterial(this.settings) })
      }.width('100%')
      Text(this.item.title).fontSize(16).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
        .width('100%').maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
      Flex({ wrap: FlexWrap.Wrap, alignItems: ItemAlign.Center, space: { main: LengthMetrics.vp(8), cross: LengthMetrics.vp(6) } }) {
        if (receiptStatus(this.item.receipt) !== '') {
          Text(receiptStatus(this.item.receipt)).fontSize(14).lineHeight(22).fontColor(this.toneColor(receiptTone(this.item.receipt)))
        }
        ForEach(receiptBadges(this.item.receipt), (badge: ReceiptBadge) => {
          this.Pill(badge.text, this.toneColor(badge.tone), this.toneBackground(badge.tone))
        }, (badge: ReceiptBadge) => badge.text)
      }.width('100%')
      ForEach(failedItems(this.item), (failed: FailedItem) => {
        this.FailedRow(failed)
      }, (failed: FailedItem) => `${failed.index}`)
    }.padding({ left: 18, right: 18, top: 8, bottom: 18 }).width('100%')
      .alignItems(HorizontalAlign.Start).backgroundColor($r('app.color.surface')).borderRadius(20)
      .transition(revealTransition(this.settings, this.index))
  }
}
