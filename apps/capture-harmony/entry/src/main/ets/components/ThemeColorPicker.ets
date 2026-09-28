import { ThemePalette, themePalette, THEME_COLOR_IDS } from '../services/ThemePalette';
import { AppPreferences } from '../services/AppPreferences';
import { pressFeedback } from '../services/InterfaceMotion';

@Component
export struct ThemeColorPicker {
  @Consume('capturePalette') palette: ThemePalette;
  @Prop selected: string = 'blue';
  @Prop busy: boolean = false;
  @Prop settings: AppPreferences = new AppPreferences();
  onSelect: (id: string) => void = () => {};

  @Builder
  private Swatch(option: ThemePalette) {
    Button({ type: ButtonType.Normal }) {
      Column({ space: 7 }) {
        Stack() {
          Row().width(30).height(30).backgroundColor(option.accent).borderRadius(15)
          if (this.selected === option.id) {
            Text('✓').fontSize(19).fontWeight(FontWeight.Bold).fontColor(option.onAccent)
          }
        }.width(38).height(38).borderRadius(19)
          .border({ width: this.selected === option.id ? 2 : 0, color: option.accent })
        Text(option.label).fontSize(11).maxLines(1)
          .fontColor(this.selected === option.id ? this.palette.accent : $r('app.color.text_secondary'))
      }.width('100%').padding({ top: 10, bottom: 10 })
    }.layoutWeight(1).padding(0).backgroundColor(Color.Transparent).borderRadius(12)
      .enabled(!this.busy).clickEffect(pressFeedback(this.settings)).accessibilitySelected(this.selected === option.id)
      .accessibilityText(`${option.label}${this.selected === option.id ? '，已选中' : ''}`)
      .onClick(() => { if (this.selected !== option.id) this.onSelect(option.id); })
  }

  build() {
    Column({ space: 10 }) {
      Row() {
        Text('主题色').fontSize(15).fontColor($r('app.color.text_primary')).layoutWeight(1)
        Text(this.palette.label).fontSize(13).fontColor(this.palette.accent)
      }.width('100%')
      Row() {
        ForEach(THEME_COLOR_IDS, (id: string) => { this.Swatch(themePalette(id)) }, (id: string) => id)
      }.width('100%')
      Row({ space: 10 }) {
        Image($r('app.media.ic_compose')).width(22).height(22).fillColor(this.palette.accent)
        Text('按钮、图标与选中状态').fontSize(12).fontColor(this.palette.accent).layoutWeight(1)
        Image($r('app.media.ic_plus')).fillColor(this.palette.onAccent)
          .padding(8).width(32).height(32).backgroundColor(this.palette.accent).borderRadius(16)
      }.width('100%').padding(12).backgroundColor(this.palette.soft).borderRadius(12)
        .accessibilityText(`${this.palette.label}配色预览`)
    }.width('100%').padding(16).backgroundColor($r('app.color.surface')).borderRadius(14)
  }
}
