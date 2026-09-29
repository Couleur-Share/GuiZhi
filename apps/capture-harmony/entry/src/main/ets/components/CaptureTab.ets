import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { pressFeedback } from '../services/InterfaceMotion';
import { CONTENT_MAX_WIDTH } from '../services/Layout';
import { DayGroup, groupByDay } from '../core/DayGroups';
import { DesktopStatus } from '../core/DesktopStatus';
import { draftKey, groupKey } from '../core/ListKeys';
import { Draft } from '../core/Protocol';
import { SendingEntry, sendingFind } from '../core/SendingState';
import { DesktopStatusLine } from './DesktopStatusLine';
import { DraftRow } from './DraftRow';
import { EmptyState } from './EmptyState';
import { GroupHeader } from './GroupHeader';

// 收集页：记录入口、待发送草稿列表（按今天 / 昨天 / 更早分组）。
// 数据与操作都由 Index 持有：这里只负责呈现，并把用户动作通过回调交回去。
@Component
export struct CaptureTab {
  @Consume('capturePalette') palette: ThemePalette;
  // 编辑面板里尚未保存的输入：非空时入口显示“继续未完成的记录”。
  @Prop input: string = '';
  // 可见的草稿，最新在前；延迟删除中的草稿已由调用方去掉。
  @Prop drafts: Draft[] = [];
  @Prop connected: boolean = false;
  @Prop paired: boolean = false;
  @Prop desktop: DesktopStatus = new DesktopStatus();
  @Prop busy: boolean = false;
  @Prop batchDone: number = 0;
  @Prop batchTotal: number = 0;
  @Prop sendings: SendingEntry[] = [];
  @Prop selecting: boolean = false;
  @Prop selectedIds: string[] = [];
  @Prop settings: AppPreferences = new AppPreferences();
  // 列表末尾为页签栏、多选栏或撤销条预留的空间。
  @Prop endOffset: number = 112;
  private scroller: ListScroller = new ListScroller();
  onCompose: () => void = () => {};
  onSendAll: () => void = () => {};
  onSend: (draft: Draft) => void = (draft: Draft): void => {};
  onEdit: (draft: Draft) => void = (draft: Draft): void => {};
  onDelete: (draft: Draft) => void = (draft: Draft): void => {};
  onEnterSelect: (draft: Draft) => void = (draft: Draft): void => {};
  onToggleSelect: (draft: Draft) => void = (draft: Draft): void => {};
  onConnect: () => void = () => {};

  private isSending(draft: Draft): boolean {
    return sendingFind(this.sendings, draft.requestId) !== undefined;
  }

  private secondsOf(draft: Draft): number {
    const entry = sendingFind(this.sendings, draft.requestId);
    return entry === undefined ? 0 : entry.seconds;
  }

  // 右滑露出：发送（未连接时改为“连接”）。只露出按钮、需要再点一下，不会因为滑过头误发送。
  @Builder
  private SendSwipe(draft: Draft) {
    Button({ type: ButtonType.Normal }) {
      Column({ space: 4 }) {
        SymbolGlyph(this.connected ? $r('sys.symbol.paperplane') : $r('sys.symbol.link'))
          .fontSize(20).fontColor([this.palette.accent]).accessibilityLevel('no')
        Text(this.connected ? '发送' : '连接').fontSize(12).fontColor(this.palette.accent)
      }
    }.width(76).height('100%').borderRadius(0).backgroundColor(this.palette.soft)
      .enabled(!this.busy && !this.isSending(draft))
      .onClick(() => {
        this.scroller.closeAllSwipeActions();
        if (this.connected) this.onSend(draft); else this.onConnect();
      })
  }

  // 左滑露出：删除。同样需要再点一下；删除后有 8 秒撤销窗口。
  @Builder
  private DeleteSwipe(draft: Draft) {
    Button({ type: ButtonType.Normal }) {
      Column({ space: 4 }) {
        SymbolGlyph($r('sys.symbol.trash')).fontSize(20).fontColor([$r('app.color.error')]).accessibilityLevel('no')
        Text('删除').fontSize(12).fontColor($r('app.color.error'))
      }
    }.width(76).height('100%').borderRadius(0).backgroundColor($r('app.color.error_soft'))
      .enabled(!this.busy && !this.isSending(draft))
      .onClick(() => {
        this.scroller.closeAllSwipeActions();
        this.onDelete(draft);
      })
  }

  @Builder
  private GroupHead(group: DayGroup<Draft>) {
    GroupHeader({ label: group.label, count: group.items.length })
  }

  @Builder
  private Intro() {
    Column({ space: 14 }) {
      Text('随手记，稍后在电脑整理。').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
      if (this.connected && this.paired && this.desktop.visible) {
        DesktopStatusLine({ desktop: this.desktop, center: false })
      }
      Button({ type: ButtonType.Normal }) {
        Row({ space: 16 }) {
          Column({ space: 8 }) {
            Text(this.input.trim() ? '继续未完成的记录' : '记下一个想法…').fontSize(19).fontWeight(FontWeight.Medium)
              .fontColor($r('app.color.text_primary'))
            Text(this.input.trim() ? this.input : '链接、文字，都可以留在这里').fontSize(13)
              .fontColor($r('app.color.text_secondary')).maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
          }.alignItems(HorizontalAlign.Start).layoutWeight(1)
          SymbolGlyph($r('sys.symbol.square_and_pencil')).fontSize(26).fontColor([this.palette.accent])
            .accessibilityLevel('no')
        }.width('100%').padding(22)
      }.width('100%').constraintSize({ minHeight: 112 }).borderRadius(20)
        .backgroundColor($r('app.color.surface')).enabled(!this.busy)
        .clickEffect(pressFeedback(this.settings)).onClick(this.onCompose)
      Row({ space: 8 }) {
        Text('待发送').fontSize(15).fontWeight(FontWeight.Medium).fontColor($r('app.color.text_primary'))
        Text(`${this.drafts.length}`).fontSize(12).fontColor($r('app.color.text_tertiary'))
        Blank()
        if (this.connected && this.drafts.length >= 2 && !this.selecting) {
          // 批量发送期间显示进度；整个过程不锁页面，可以继续记录。
          Button(this.batchTotal > 0 ? `发送中 ${this.batchDone}/${this.batchTotal}` : '全部发送', { type: ButtonType.Normal })
            .constraintSize({ minHeight: 44 }).fontSize(14).padding({ left: 14, right: 14 }).borderRadius(12)
            .backgroundColor(this.palette.soft).fontColor(this.palette.accent)
            .enabled(this.batchTotal === 0 && !this.busy).clickEffect(pressFeedback(this.settings))
            .accessibilityText(this.batchTotal > 0 ? `全部发送进行中，已处理 ${this.batchDone} 条，共 ${this.batchTotal} 条` : `全部发送，共 ${this.drafts.length} 条`)
            .onClick(this.onSendAll)
        } else if (!this.selecting) {
          Text('仅保存在本机').fontSize(12).fontColor($r('app.color.text_tertiary'))
        }
      }.width('100%')
    }.width('100%').padding({ top: 4, bottom: 0 })
  }

  @Builder
  private ConnectCard() {
    Button({ type: ButtonType.Normal }) {
      Row({ space: 12 }) {
        SymbolGlyph($r('sys.symbol.link')).fontSize(20).fontColor([$r('app.color.text_secondary')]).accessibilityLevel('no')
        Column({ space: 4 }) {
          Text('连接你的电脑').fontSize(14).fontColor($r('app.color.text_primary'))
          Text('随时收集，配对后再发送').fontSize(12).fontColor($r('app.color.text_tertiary'))
        }.layoutWeight(1).alignItems(HorizontalAlign.Start)
        SymbolGlyph($r('sys.symbol.chevron_right')).fontSize(14).fontColor([$r('app.color.text_tertiary')])
          .accessibilityLevel('no')
      }.padding(16).width('100%')
    }.width('100%').borderRadius(20).backgroundColor($r('app.color.surface'))
      .clickEffect(pressFeedback(this.settings)).onClick(this.onConnect)
  }

  build() {
    List({ space: 14, scroller: this.scroller }) {
      ListItem() { this.Intro() }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
      if (this.drafts.length === 0) {
        ListItem() {
          EmptyState({ icon: $r('sys.symbol.inbox'), title: '从第一条收集开始',
            detail: '点击上方记录，或从其他应用\n分享链接与文字到归知。' })
        }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
      }
      ForEach(groupByDay(this.drafts, (draft: Draft): number => draft.createdAt, Date.now()), (group: DayGroup<Draft>) => {
        ListItemGroup({ header: this.GroupHead(group) }) {
          ForEach(group.items, (draft: Draft) => {
            ListItem() {
              DraftRow({ draft: draft, settings: this.settings, busy: this.busy, connected: this.connected,
                sending: this.isSending(draft), sendingSeconds: this.secondsOf(draft),
                selecting: this.selecting, selected: this.selectedIds.includes(draft.requestId),
                onSend: () => { this.onSend(draft); },
                onEdit: () => { this.onEdit(draft); },
                onRemove: () => { this.onDelete(draft); },
                onConnect: () => { this.onConnect(); },
                onToggle: () => { this.onToggleSelect(draft); },
                onEnterSelect: () => { this.onEnterSelect(draft); } })
            }.swipeAction({ start: this.SendSwipe(draft), end: this.DeleteSwipe(draft), edgeEffect: SwipeEdgeEffect.Spring })
          }, (draft: Draft) => draftKey(draft))
        }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH }).backgroundColor($r('app.color.surface'))
          .borderRadius(20).clip(true)
          .divider({ strokeWidth: 1, color: $r('app.color.border_subtle'), startMargin: 64, endMargin: 16 })
      }, (group: DayGroup<Draft>) => groupKey(group, draftKey))
      if (!this.connected) {
        ListItem() { this.ConnectCard() }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
      }
    }.width('100%').height('100%').padding({ left: 20, right: 20 }).alignListItem(ListItemAlign.Center)
      .contentEndOffset(this.endOffset).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
  }
}
