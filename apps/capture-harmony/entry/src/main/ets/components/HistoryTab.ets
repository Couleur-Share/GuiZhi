import { ThemePalette } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { CONTENT_MAX_WIDTH } from '../services/Layout';
import { DayGroup, groupByDay } from '../core/DayGroups';
import { HistoryItem, historyKey } from '../core/HistoryView';
import { groupKey } from '../core/ListKeys';
import { PrimaryAction, SecondaryAction } from './Actions';
import { EmptyState } from './EmptyState';
import { GroupHeader } from './GroupHeader';
import { SentRow } from './SentRow';

// 近期页：中转回执与本机发送记录合并后的列表（按今天 / 昨天 / 更早分组），下拉刷新，底部可加载更多。
@Component
export struct HistoryTab {
  @Consume('capturePalette') palette: ThemePalette;
  // 下拉刷新的进行状态与 Index 双向同步：控制方法负责在结束后复位。
  @Link pulling: boolean;
  @Prop items: HistoryItem[] = [];
  // 服务端回执条数（未合并）：决定“已显示全部/最近 N 条”的页脚。
  @Prop receiptCount: number = 0;
  @Prop connected: boolean = false;
  @Prop paired: boolean = false;
  @Prop loaded: boolean = false;
  @Prop statusError: string = '';
  @Prop more: boolean = false;
  @Prop loadingMore: boolean = false;
  @Prop refreshing: boolean = false;
  @Prop busy: boolean = false;
  @Prop sentLogWarning: string = '';
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop endOffset: number = 112;
  onConnect: () => void = () => {};
  onPull: () => void = () => {};
  onLoadMore: () => void = () => {};
  onCopyId: (item: HistoryItem) => void = (item: HistoryItem): void => {};
  onCopyUrl: (url: string) => void = (url: string): void => {};
  onOpenUrl: (url: string) => void = (url: string): void => {};
  onCopyReason: (summary: string) => void = (summary: string): void => {};

  @Builder
  private GroupHead(group: DayGroup<HistoryItem>) {
    GroupHeader({ label: group.label, count: group.items.length })
  }

  @Builder
  private Intro() {
    Column({ space: 14 }) {
      Text('追踪接收与处理结果 · 下拉刷新').fontSize(14).fontColor($r('app.color.text_secondary')).width('100%')
      if (this.sentLogWarning !== '') {
        Text(this.sentLogWarning).fontSize(13).lineHeight(20).fontColor($r('app.color.warning')).width('100%')
          .padding(12).backgroundColor($r('app.color.warning_soft')).borderRadius(14)
      }
      if (!this.connected) {
        EmptyState({ icon: $r('sys.symbol.link'), title: '连接后查看投递进度',
          detail: '本机草稿不会自动发送，\n你可以先收集，再连接电脑。' })
        PrimaryAction({ label: '连接电脑', icon: $r('sys.symbol.link'), active: !this.busy,
          settings: this.settings, onAction: this.onConnect })
      } else if (!this.loaded && !this.statusError) {
        EmptyState({ icon: $r('sys.symbol.clock'), title: this.paired ? '正在读取记录' : '等待电脑确认',
          detail: '配对完成后，记录会显示在这里。' })
      } else if (this.loaded && this.receiptCount === 0 && !this.statusError) {
        EmptyState({ icon: $r('sys.symbol.clock'), title: '还没有发送记录', detail: '在收集箱中选一条内容，点击发送。' })
      }
    }.width('100%').padding({ top: 4 })
  }

  @Builder
  private Footer() {
    Column({ space: 14 }) {
      if (this.more) {
        SecondaryAction({ label: this.loadingMore ? '正在加载…' : '加载更多', active: !this.loadingMore,
          settings: this.settings, onAction: this.onLoadMore })
      }
      Text(`${this.more ? '已显示最近' : '已显示全部'} ${this.receiptCount} 条 · 采集失败可在电脑重试`)
        .fontSize(12).fontColor($r('app.color.text_tertiary'))
    }.width('100%')
  }

  build() {
    Refresh({ refreshing: $$this.pulling }) {
      List({ space: 14 }) {
        ListItem() { this.Intro() }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
        ForEach(groupByDay(this.items, (item: HistoryItem): number => item.receipt.createdAt, Date.now()), (group: DayGroup<HistoryItem>) => {
          ListItemGroup({ header: this.GroupHead(group) }) {
            ForEach(group.items, (item: HistoryItem) => {
              ListItem() {
                SentRow({ item: item, settings: this.settings,
                  onCopyId: () => { this.onCopyId(item); },
                  onCopyUrl: (url: string) => { this.onCopyUrl(url); },
                  onOpenUrl: (url: string) => { this.onOpenUrl(url); },
                  onCopyReason: (summary: string) => { this.onCopyReason(summary); } })
              }
            }, (item: HistoryItem) => historyKey(item))
          }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH }).backgroundColor($r('app.color.surface'))
            .borderRadius(20).clip(true)
            .divider({ strokeWidth: 1, color: $r('app.color.border_subtle'), startMargin: 64, endMargin: 16 })
        }, (group: DayGroup<HistoryItem>) => groupKey(group, historyKey))
        if (this.loaded && this.receiptCount > 0) {
          ListItem() { this.Footer() }.width('100%').constraintSize({ maxWidth: CONTENT_MAX_WIDTH })
        }
      }.width('100%').height('100%').padding({ left: 20, right: 20 }).alignListItem(ListItemAlign.Center)
        .contentEndOffset(this.endOffset).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
    }.width('100%').height('100%').pullToRefresh(this.connected && !this.busy && !this.refreshing)
      .onRefreshing(() => { this.onPull(); })
  }
}
