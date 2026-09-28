import { AppPreferences } from '../services/AppPreferences';
import { ThemePalette } from '../services/ThemePalette';
import { animationSpeedLabel, pressFeedback, revealTransition } from '../services/InterfaceMotion';
import { materialAvailable, materialLabel } from '../services/ImmersiveAppearance';
import { ThemeColorPicker } from './ThemeColorPicker';
import { SettingChoiceRow, SettingSwitchRow } from './SettingsRows';

@Component
export struct AppearanceSettings {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop settings: AppPreferences = new AppPreferences();
  @Prop busy: boolean = false;
  @Prop holdingMessage: string = '';
  onChange: (key: string, value: string) => void = () => {};
  onRetryHolding: () => void = () => {};

  @Builder
  private Separator() {
    Divider().color($r('app.color.border_subtle')).margin({ left: 50 }).width('82%')
  }

  @Builder
  private Caption(text: string) {
    Text(text).fontSize(12).lineHeight(19).fontColor($r('app.color.text_secondary')).width('100%').padding(8)
  }

  build() {
    Column({ space: 12 }) {
      ThemeColorPicker({ selected: this.settings.themeColor, settings: this.settings, busy: this.busy,
        onSelect: (id: string) => this.onChange('themeColor', id) })
      Column() {
        SettingChoiceRow({ title: '深浅模式', icon: $r('sys.symbol.circle_righthalf_inset_filled'), settings: this.settings,
          value: this.settings.theme === 'system' ? '跟随系统' : this.settings.theme === 'dark' ? '深色' : '浅色', interactive: !this.busy,
          items: [
            { value: '跟随系统', action: () => this.onChange('theme', 'system') },
            { value: '浅色', action: () => this.onChange('theme', 'light') },
            { value: '深色', action: () => this.onChange('theme', 'dark') }
          ] })
        this.Separator()
        SettingChoiceRow({ title: '动画速度', icon: $r('sys.symbol.livephoto'), settings: this.settings,
          value: this.settings.reduceMotion ? '已减少动效' : animationSpeedLabel(this.settings.animationSpeed),
          interactive: !this.busy && !this.settings.reduceMotion, items: [
            { value: '轻快', action: () => this.onChange('speed', 'fast') },
            { value: '标准', action: () => this.onChange('speed', 'normal') },
            { value: '舒缓', action: () => this.onChange('speed', 'relaxed') }
          ] })
        this.Separator()
        SettingSwitchRow({ title: '减少动效', value: this.settings.reduceMotion, settings: this.settings,
          interactive: !this.busy, onChange: (checked: boolean) => this.onChange('motion', `${checked}`) })
      }.width('100%').backgroundColor($r('app.color.surface')).borderRadius(20).transition(revealTransition(this.settings, 1))
      this.Caption('减少动效会关闭应用的位移、缩放和过渡。重新开启后，保留原来的动画速度。')
      Text('材质与反馈').fontSize(13).fontColor($r('app.color.text_secondary')).width('100%').padding({ left: 8, top: 8 })
      Column() {
        SettingChoiceRow({ title: '沉浸光感', icon: $r('sys.symbol.rectangle_on_rectangle_fill'), settings: this.settings,
          value: materialLabel(this.settings.material), interactive: !this.busy, items: [
            { value: '自动（按组件）', action: () => this.onChange('material', 'system') },
            { value: '超薄', action: () => this.onChange('material', 'ultraThin') },
            { value: '薄', action: () => this.onChange('material', 'thin') },
            { value: '常规', action: () => this.onChange('material', 'regular') },
            { value: '厚', action: () => this.onChange('material', 'thick') },
            { value: '超厚', action: () => this.onChange('material', 'ultraThick') },
            { value: '关闭', action: () => this.onChange('material', 'off') }
          ] })
        this.Separator()
        SettingChoiceRow({ title: '底栏样式', icon: $r('sys.symbol.circle_on_circle_filled'), settings: this.settings,
          value: this.settings.tabStyle === 'standard' ? '贴底导航' : '悬浮胶囊', interactive: !this.busy, items: [
            { value: '悬浮胶囊', action: () => this.onChange('tabStyle', 'floating') },
            { value: '贴底导航', action: () => this.onChange('tabStyle', 'standard') }
          ] })
        this.Separator()
        SettingSwitchRow({ title: '按压反馈', icon: $r('sys.symbol.hand_draw'), value: this.settings.lightFeedback,
          settings: this.settings, interactive: !this.busy, onChange: (checked: boolean) => this.onChange('light', `${checked}`) })
      }.width('100%').backgroundColor($r('app.color.surface')).borderRadius(20).transition(revealTransition(this.settings, 2))
      this.Caption(materialAvailable() ? '光感用于底栏、菜单和弹层。按压反馈同时控制按钮形变，效果随系统与设备调整。' :
        '当前设备使用普通系统材质。按压反馈仍可控制按钮形变。')
      Text('单手操作').fontSize(13).fontColor($r('app.color.text_secondary')).width('100%').padding({ left: 8, top: 8 })
      Column() {
        SettingSwitchRow({ title: '智感握姿', icon: $r('sys.symbol.hand_draw'), value: this.settings.handedness,
          settings: this.settings, interactive: !this.busy, onChange: (checked: boolean) => this.onChange('hand', `${checked}`) })
        Row({ space: 8 }) {
          Text(this.settings.handedness ? this.holdingMessage : '已关闭 · 新建入口使用默认位置')
            .fontSize(12).lineHeight(19).fontColor($r('app.color.text_secondary')).layoutWeight(1)
          if (this.settings.handedness) {
            Button('重新检测').height(44).fontSize(12).fontColor(this.palette.accent).backgroundColor(Color.Transparent)
              .clickEffect(pressFeedback(this.settings)).enabled(!this.busy).onClick(this.onRetryHolding)
          }
        }.padding({ left: 16, right: 12, bottom: 8 }).width('100%')
      }.width('100%').backgroundColor($r('app.color.surface')).borderRadius(20).transition(revealTransition(this.settings, 3))
      this.Caption('新建入口随握持手切换；宽屏底栏在系统支持时跟手。仅前台感知，不保存或上传握持数据。')
    }.width('100%')
  }
}
