import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { animationSpeedLabel, pressFeedback, revealTransition } from '../services/InterfaceMotion';
import { AppearanceSettings } from './AppearanceSettings';
import { SettingChoiceRow, SettingSwitchRow, SettingsRouteRow } from './SettingsRows';

@Component
export struct SettingsPage {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop busy: boolean = false;
  @Prop holdingMessage: string = '等待握持识别';
  @State routes: NavPathStack = new NavPathStack();
  onRetryHolding: () => void = () => {};
  onChange: (settings: AppPreferences) => void = () => {};
  onReset: () => void = () => {};
  onConnect: () => void = () => {};
  onDetail: (visible: boolean) => void = () => {};

  private update(key: string, value: string): void {
    const next = JSON.parse(JSON.stringify(this.settings)) as AppPreferences;
    switch (key) {
      case 'themeColor': next.themeColor = value; break;
      case 'theme': next.theme = value; break;
      case 'material': next.material = value; break;
      case 'tabStyle': next.tabStyle = value; break;
      case 'speed': next.animationSpeed = value; break;
      case 'light': next.lightFeedback = value === 'true'; break;
      case 'motion': next.reduceMotion = value === 'true'; break;
      case 'hand': next.handedness = value === 'true'; break;
      case 'mode': next.defaultMode = value === 'urls' ? 'urls' : value === 'text' ? 'text' : 'auto'; break;
      case 'preview': next.previewLines = value === '6' ? 6 : 3; break;
      case 'count': next.showCount = value === 'true'; break;
    }
    this.onChange(next);
  }

  private open(name: string): void {
    if (this.routes.size() === 0) this.routes.pushPath({ name: name }, !this.settings.reduceMotion);
  }
  private back(): boolean { this.routes.pop(!this.settings.reduceMotion); return true; }
  private connect(): void { this.routes.clear(false); this.onDetail(false); this.onConnect(); }

  @Builder
  private Paragraph(text: string) {
    Text(text).fontSize(14).lineHeight(23).fontColor($r('app.color.text_secondary'))
      .padding(20).width('100%').backgroundColor($r('app.color.surface')).borderRadius(20)
  }

  @Builder
  private CollectionPreferences() {
    Column() {
      SettingChoiceRow({ title: '默认收集方式', icon: $r('sys.symbol.doc_text'), settings: this.settings,
        value: this.settings.defaultMode === 'auto' ? '自动识别' : this.settings.defaultMode === 'urls' ? '采集链接' : '保存文字',
        interactive: !this.busy, items: [
          { value: '自动识别', action: () => this.update('mode', 'auto') },
          { value: '采集链接', action: () => this.update('mode', 'urls') },
          { value: '保存文字', action: () => this.update('mode', 'text') }
        ] })
      Divider().color($r('app.color.border_subtle')).margin({ left: 50 }).width('82%')
      SettingChoiceRow({ title: '内容预览', icon: $r('sys.symbol.list_square_bill'), settings: this.settings,
        value: this.settings.previewLines === 3 ? '精简 · 3 行' : '详细 · 6 行', interactive: !this.busy, items: [
          { value: '精简 · 3 行', action: () => this.update('preview', '3') },
          { value: '详细 · 6 行', action: () => this.update('preview', '6') }
        ] })
      Divider().color($r('app.color.border_subtle')).margin({ left: 50 }).width('82%')
      SettingSwitchRow({ title: '编辑时显示字数', icon: $r('sys.symbol.doc_text'), settings: this.settings,
        value: this.settings.showCount, interactive: !this.busy, onChange: (checked: boolean) => this.update('count', `${checked}`) })
    }.width('100%').backgroundColor($r('app.color.surface')).borderRadius(20)
    Text('默认方式用于下一条新收集，已有内容保持原方式。').fontSize(12).lineHeight(20)
      .fontColor($r('app.color.text_secondary')).width('100%').padding(8)
  }

  @Builder
  private Destination(name: string) {
    NavDestination() {
      Scroll() {
        Column({ space: 12 }) {
          if (name === 'appearance') {
            AppearanceSettings({ settings: this.settings, busy: this.busy, holdingMessage: this.holdingMessage,
              onRetryHolding: this.onRetryHolding, onChange: (key: string, value: string) => this.update(key, value) })
          } else if (name === 'capture') {
            this.CollectionPreferences()
          } else if (name === 'privacy') {
            SettingsRouteRow({ title: '管理电脑连接', detail: '检查配对状态与连接', icon: $r('sys.symbol.link'),
              settings: this.settings, onOpen: () => this.connect() })
            this.Paragraph('草稿保存在本机，确认发送后才上传。你可以离线记录，连接电脑后再发送。')
            this.Paragraph('设备凭证由系统安全存储保护。中转服务可以读取暂存内容；卸载应用会移除本机草稿。')
          } else {
            Column({ space: 12 }) {
              Image($r('app.media.app_icon')).width(64).height(64).borderRadius(16)
              Text('归知收集').fontSize(22).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary'))
              Text('0.3.0 · 邀请测试').fontSize(13).fontColor($r('app.color.text_secondary'))
              Text('先收集，再整理。').fontSize(14).fontColor($r('app.color.text_secondary'))
            }.width('100%').padding(28).backgroundColor($r('app.color.surface')).borderRadius(20)
            Button('恢复默认设置', { type: ButtonType.Normal }).height(56).width('100%').fontSize(15)
              .fontColor($r('app.color.text_primary')).backgroundColor($r('app.color.surface')).borderRadius(20)
              .clickEffect(pressFeedback(this.settings)).enabled(!this.busy).onClick(this.onReset)
            Text('只重置外观与收集偏好，保留草稿和电脑连接。').fontSize(12).lineHeight(20)
              .fontColor($r('app.color.text_secondary')).padding(8).width('100%')
          }
        }.width('100%').padding({ left: 20, right: 20, top: 12, bottom: 32 })
      }.width('100%').height('100%').align(Alignment.TopStart).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
    }.title(name === 'appearance' ? '外观与动效' : name === 'capture' ? '收集偏好' : name === 'privacy' ? '连接与隐私' : '关于归知')
      .backgroundColor($r('app.color.page_background')).onBackPressed(() => this.back())
      .onShown(() => this.onDetail(true)).onHidden(() => this.onDetail(false))
  }

  build() {
    Navigation(this.routes) {
      Scroll() {
        Column({ space: 12 }) {
          Text('设置').fontSize(30).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary'))
            .width('100%').padding({ left: 4, top: 16, bottom: 14 })
          SettingsRouteRow({ title: '外观与动效', detail: `${this.palette.label} · ${this.settings.reduceMotion ? '减少动效' : animationSpeedLabel(this.settings.animationSpeed) + '动效'}`,
            icon: $r('sys.symbol.paintpalette_fill'), settings: this.settings, onOpen: () => this.open('appearance') })
            .transition(revealTransition(this.settings))
          SettingsRouteRow({ title: '收集偏好', detail: '默认方式、预览行数与字数', icon: $r('sys.symbol.doc_text'),
            settings: this.settings, onOpen: () => this.open('capture') }).transition(revealTransition(this.settings, 1))
          SettingsRouteRow({ title: '连接与隐私', detail: '电脑配对与本机数据', icon: $r('sys.symbol.link'),
            settings: this.settings, onOpen: () => this.open('privacy') }).transition(revealTransition(this.settings, 2))
          SettingsRouteRow({ title: '关于归知', detail: '版本信息与恢复默认', icon: $r('sys.symbol.info_circle'),
            settings: this.settings, onOpen: () => this.open('about') }).transition(revealTransition(this.settings, 3))
        }.padding({ left: 20, right: 20, bottom: 112 }).width('100%')
      }.height('100%').width('100%').align(Alignment.TopStart).scrollBar(BarState.Off).edgeEffect(EdgeEffect.Spring)
    }.mode(NavigationMode.Stack).hideTitleBar(true).navDestination(this.Destination).width('100%').height('100%')
      .backgroundColor($r('app.color.page_background'))
  }
}
