import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences, previewLineCount } from '../services/AppPreferences';
import { animateInterface, pressFeedback } from '../services/InterfaceMotion';
import { panelMaterial } from '../services/ImmersiveAppearance';
import { Draft } from '../core/Protocol';
import { relativeTime } from '../core/RelativeTime';
import { sendingLabel, sendingSlowHint } from '../core/SendingState';

// 收集箱里的一条草稿（紧凑行）。点击展开全文与详情（多选中改为切换选中），长按进入多选；
// 行尾的发送按钮与更多菜单能完成全部操作，外层 ListItem 的滑动手势（右滑发送、左滑删除）只是快捷方式，读屏用户走按钮与菜单。
@Component
export struct DraftRow {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop draft: Draft = new Draft();
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop busy: boolean = false;
  @Prop connected: boolean = false;
  // 这一条正在网络发送：只锁定本行的发送与菜单，页面其他操作不受影响。
  @Prop sending: boolean = false;
  @Prop sendingSeconds: number = 0;
  @Prop selecting: boolean = false;
  @Prop selected: boolean = false;
  @State expanded: boolean = false;
  onSend: () => void = () => {};
  onEdit: () => void = () => {};
  onRemove: () => void = () => {};
  onConnect: () => void = () => {};
  onToggle: () => void = () => {};
  onEnterSelect: () => void = () => {};

  private source(): string {
    const match = /https?:\/\/([^\s/]+)/i.exec(this.draft.input);
    return match ? match[1] : '文字笔记';
  }

  private isNote(): boolean {
    return this.draft.mode === 'text' || this.source() === '文字笔记';
  }

  private statusText(): string {
    if (this.sending) return sendingLabel(this.sendingSeconds);
    if (this.draft.attempted) return '待确认结果';
    return this.draft.lastError ? '发送失败，可重试' : '尚未发送';
  }

  private statusColor(): ResourceColor {
    if (this.sending) return this.palette.accent;
    if (this.draft.attempted) return $r('app.color.warning');
    return this.draft.lastError ? $r('app.color.error') : $r('app.color.text_secondary');
  }

  // 发送图标按钮的无障碍名称：图标本身不带文字，名称要说明它现在会做什么。
  private sendName(): string {
    if (this.sending) return '正在发送';
    if (this.busy) return '处理中';
    if (!this.connected) return '连接电脑';
    return this.draft.attempted ? '重试发送' : '发送';
  }

  // 多选中整行读作一个复选项：状态 + 内容开头。
  private selectionName(): string {
    const text = this.draft.input.length > 60 ? `${this.draft.input.substring(0, 60)}…` : this.draft.input;
    return `${this.statusText()}，${text}`;
  }

  private toggleExpanded(): void {
    animateInterface(this.getUIContext(), this.settings, () => { this.expanded = !this.expanded; });
  }

  private menu(): MenuElement[] {
    const items: MenuElement[] = [];
    if (!this.draft.attempted) items.push({ value: '编辑内容', action: () => { this.onEdit(); } });
    items.push({ value: this.expanded ? '收起详情' : '展开详情', action: () => { this.toggleExpanded(); } });
    items.push({ value: '多选', action: () => { this.onEnterSelect(); } });
    items.push({ value: '删除本机草稿', action: () => { this.onRemove(); } });
    return items;
  }

  @Builder
  private Leading() {
    if (this.selecting) {
      SymbolGlyph(this.selected ? $r('sys.symbol.checkmark_circle_fill') : $r('sys.symbol.circle'))
        .fontSize(26).fontColor([this.selected ? this.palette.accent : $r('app.color.text_tertiary')])
        .accessibilityLevel('no')
    } else {
      Column() {
        SymbolGlyph(this.isNote() ? $r('sys.symbol.square_and_pencil') : $r('sys.symbol.link'))
          .fontSize(18).fontColor([this.palette.accent]).accessibilityLevel('no')
      }.width(36).height(36).justifyContent(FlexAlign.Center).backgroundColor(this.palette.soft).borderRadius(12)
    }
  }

  @Builder
  private Actions() {
    Row({ space: 2 }) {
      Button({ type: ButtonType.Circle }) {
        if (this.sending) {
          LoadingProgress().width(22).height(22).color(this.palette.accent)
        } else {
          SymbolGlyph(this.connected ? $r('sys.symbol.paperplane') : $r('sys.symbol.link'))
            .fontSize(20).fontColor([this.palette.accent])
        }
      }.width(44).height(44).backgroundColor(this.palette.soft).clickEffect(pressFeedback(this.settings))
        .enabled(!this.busy && !this.sending).accessibilityText(this.sendName())
        .onClick(() => { if (this.connected) this.onSend(); else this.onConnect(); })
      Button({ type: ButtonType.Circle }) {
        SymbolGlyph($r('sys.symbol.more')).fontSize(20).fontColor([$r('app.color.text_secondary')])
      }.width(44).height(44).backgroundColor(Color.Transparent).clickEffect(pressFeedback(this.settings))
        .enabled(!this.busy && !this.sending).accessibilityText('草稿操作')
        .bindMenu(this.menu(), { systemMaterial: panelMaterial(this.settings) })
    }
  }

  build() {
    Row({ space: 12 }) {
      this.Leading()
      Column({ space: 3 }) {
        if (this.expanded) {
          Text(this.draft.input).fontSize(15).lineHeight(23).fontColor($r('app.color.text_primary'))
            .width('100%').copyOption(CopyOptions.LocalDevice)
        } else {
          Text(this.draft.input).fontSize(15).lineHeight(23).fontColor($r('app.color.text_primary'))
            .width('100%').maxLines(previewLineCount(this.settings)).textOverflow({ overflow: TextOverflow.Ellipsis })
        }
        Text() {
          Span(this.statusText()).fontColor(this.statusColor())
          Span(` · ${relativeTime(this.draft.createdAt, Date.now())}`).fontColor($r('app.color.text_tertiary'))
        }.fontSize(12).lineHeight(18).width('100%').maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
        if (this.sending) {
          if (sendingSlowHint(this.sendingSeconds) !== '') {
            Text(sendingSlowHint(this.sendingSeconds)).fontSize(12).lineHeight(18).fontColor($r('app.color.text_secondary'))
              .width('100%')
          }
        } else if (this.draft.lastError) {
          // 失败原因保持可见：折叠时一行，展开后完整并可复制。
          if (this.expanded) {
            Text(`上次发送失败：${this.draft.lastError}`).fontSize(12).lineHeight(18).fontColor($r('app.color.error'))
              .width('100%').copyOption(CopyOptions.LocalDevice)
          } else {
            Text(`上次发送失败：${this.draft.lastError}`).fontSize(12).lineHeight(18).fontColor($r('app.color.error'))
              .width('100%').maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
          }
        } else if (this.expanded && this.draft.attempted) {
          Text('已发出但未收到回执，重试不会重复保存').fontSize(12).lineHeight(18)
            .fontColor($r('app.color.text_secondary')).width('100%')
        }
      }.layoutWeight(1).alignItems(HorizontalAlign.Start)
      if (!this.selecting) {
        this.Actions()
      }
    }.width('100%').constraintSize({ minHeight: 64 }).padding({ left: 16, right: 8, top: 10, bottom: 10 })
      .alignItems(VerticalAlign.Center)
      .backgroundColor(this.selected ? this.palette.soft : Color.Transparent)
      .accessibilityGroup(this.selecting).accessibilityText(this.selecting ? this.selectionName() : '')
      .accessibilityRole(this.selecting ? AccessibilityRoleType.CHECKBOX : AccessibilityRoleType.LIST_ITEM)
      .accessibilityChecked(this.selecting && this.selected)
      .onClick(() => { if (this.selecting) this.onToggle(); else this.toggleExpanded(); })
      .gesture(LongPressGesture({ repeat: false }).onAction(() => { if (!this.selecting) this.onEnterSelect(); }))
  }
}
