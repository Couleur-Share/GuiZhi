import { LengthMetrics } from '@kit.ArkUI';
import { AppPreferences, previewLineCount } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { animateInterface, pressFeedback } from '../services/InterfaceMotion';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { ReceiptBadge, ReceiptTone, receiptBadges, receiptLabel, receiptTone } from '../core/Protocol';
import { FailedItem, HistoryItem, failedItems } from '../core/HistoryView';
import { relativeTime } from '../core/RelativeTime';

// 近期页的一条发送记录（紧凑行）：图标 + 内容预览 + “状态 · 时间”。点击展开计数徽标；
// 失败原因始终可见（逐项一行，可复制、可在浏览器打开），不需要展开才能看到出了什么问题。
@Component
export struct SentRow {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop item: HistoryItem;
  @Prop settings: AppPreferences = new AppPreferences();
  @State expanded: boolean = false;
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

  private toneIcon(tone: ReceiptTone): Resource {
    if (tone === 'success') return $r('sys.symbol.checkmark_circle');
    if (tone === 'warning') return $r('sys.symbol.exclamationmark_triangle');
    if (tone === 'error') return $r('sys.symbol.exclamationmark_circle');
    return $r('sys.symbol.clock');
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

  private toggleExpanded(): void {
    animateInterface(this.getUIContext(), this.settings, () => { this.expanded = !this.expanded; });
  }

  private menu(): MenuElement[] {
    return [
      { value: this.expanded ? '收起详情' : '展开详情', action: () => { this.toggleExpanded(); } },
      { value: '复制请求编号', action: () => { this.onCopyId(); } }
    ];
  }

  // “来源 · 状态 · 时间”：没有本机记录时没有来源。
  private sourcePrefix(): string {
    return this.item.source === '' ? '' : `${this.item.source} · `;
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
    Column({ space: 8 }) {
      Row({ space: 12 }) {
        Column() {
          SymbolGlyph(this.toneIcon(receiptTone(this.item.receipt))).fontSize(18)
            .fontColor([this.toneColor(receiptTone(this.item.receipt))]).accessibilityLevel('no')
        }.width(36).height(36).justifyContent(FlexAlign.Center)
          .backgroundColor(this.toneBackground(receiptTone(this.item.receipt))).borderRadius(12)
        Column({ space: 3 }) {
          if (this.expanded) {
            Text(this.item.title).fontSize(15).lineHeight(23).fontWeight(FontWeight.Medium)
              .fontColor($r('app.color.text_primary')).width('100%').copyOption(CopyOptions.LocalDevice)
          } else {
            Text(this.item.title).fontSize(15).lineHeight(23).fontWeight(FontWeight.Medium)
              .fontColor($r('app.color.text_primary')).width('100%')
              .maxLines(previewLineCount(this.settings)).textOverflow({ overflow: TextOverflow.Ellipsis })
          }
          Text() {
            Span(`${this.sourcePrefix()}${receiptLabel(this.item.receipt)}`)
              .fontColor(this.toneColor(receiptTone(this.item.receipt)))
            Span(` · ${relativeTime(this.item.receipt.createdAt, Date.now())}`).fontColor($r('app.color.text_tertiary'))
          }.fontSize(12).lineHeight(18).width('100%').maxLines(this.expanded ? 3 : 1)
            .textOverflow({ overflow: TextOverflow.Ellipsis })
        }.layoutWeight(1).alignItems(HorizontalAlign.Start)
        Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.more')).fontSize(20).fontColor([$r('app.color.text_secondary')])
        }.width(44).height(44).backgroundColor(Color.Transparent).clickEffect(pressFeedback(this.settings))
          .accessibilityText('记录操作')
          .bindMenu(this.menu(), { systemMaterial: panelMaterial(this.settings) })
      }.width('100%').alignItems(VerticalAlign.Center)
        .onClick(() => { this.toggleExpanded(); })
      if (this.expanded && receiptBadges(this.item.receipt).length > 0) {
        Flex({ wrap: FlexWrap.Wrap, alignItems: ItemAlign.Center, space: { main: LengthMetrics.vp(8), cross: LengthMetrics.vp(6) } }) {
          ForEach(receiptBadges(this.item.receipt), (badge: ReceiptBadge) => {
            this.Pill(badge.text, this.toneColor(badge.tone), this.toneBackground(badge.tone))
          }, (badge: ReceiptBadge) => badge.text)
        }.width('100%').padding({ left: 48 })
      }
      if (failedItems(this.item).length > 0) {
        // 缩进到文字列，与内容预览对齐。
        Column({ space: 8 }) {
          ForEach(failedItems(this.item), (failed: FailedItem) => {
            this.FailedRow(failed)
          }, (failed: FailedItem) => `${failed.index}`)
        }.width('100%').padding({ left: 48 })
      }
    }.width('100%').constraintSize({ minHeight: 64 }).padding({ left: 16, right: 8, top: 10, bottom: 10 })
  }
}
