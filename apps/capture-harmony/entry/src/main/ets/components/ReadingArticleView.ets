import { ReadingArticle, safeReadingLink } from '../core/ReadingModel';
import { ReadingLibrary } from '../core/ReadingLibrary';
import { ReadingBlock, readingBlocks, readingImageIds } from '../core/ReadingReader';
import { ReadingImage } from './ReadingImage';
import { SecondaryAction } from './Actions';
import { AppPreferences } from '../services/AppPreferences';
import { common } from '@kit.AbilityKit';

@Component
export struct ReadingArticleView {
  article: ReadingArticle = { schema: 1, id: '', title: '', content: '', summary: null, itemType: 'note',
    status: 'active', collectionName: '', tags: [], sourceUrl: null };
  library: ReadingLibrary | null = null;
  @Prop busy: boolean = false;
  @Prop imageRevision: number = 0;
  @Prop settings: AppPreferences = new AppPreferences();
  @State blocks: ReadingBlock[] = [];
  @State page: number = 0;
  @State error: string = '';
  onBack: () => void = () => {};
  onSaveImages: () => void = () => {};
  aboutToAppear(): void { this.blocks = readingBlocks(this.article.content); }
  private async openSource(): Promise<void> {
    if (!this.article.sourceUrl || !safeReadingLink(this.article.sourceUrl)) return;
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      await context.openLink(this.article.sourceUrl, { appLinkingOnly: false });
    } catch { this.error = '无法打开来源链接，请长按下方链接复制'; }
  }
  build() {
    Column({ space: 14 }) {
      Row({ space: 8 }) {
        SecondaryAction({ label: '返回列表', settings: this.settings, onAction: this.onBack })
        if (readingImageIds(this.article.content).length > 0) {
          SecondaryAction({ label: '离线保存图片', active: !this.busy, settings: this.settings, onAction: this.onSaveImages })
        }
      }.width('100%')
      Text(this.article.title || '无标题').fontSize(26).fontWeight(FontWeight.Bold).fontColor($r('app.color.text_primary')).width('100%').copyOption(CopyOptions.LocalDevice)
      Text(`${this.article.collectionName} · ${this.article.tags.join('、')}`).fontSize(13).fontColor($r('app.color.text_secondary')).width('100%')
      Text('正文已保存在本机；图片需单独保存。远程图片、脚本和嵌入页面不会自动加载。')
        .fontSize(12).fontColor($r('app.color.text_secondary')).width('100%')
      ForEach(this.blocks.slice(this.page * 30, (this.page + 1) * 30), (block: ReadingBlock) => {
        if (block.kind === 'image') {
          ReadingImage({ objectId: block.imageId, label: block.text, library: this.library, revision: this.imageRevision })
        } else if (block.kind === 'external') {
          Text(`远程图片未同步：${block.text}`).fontSize(13).fontColor($r('app.color.text_secondary')).width('100%')
        } else {
          Text(block.text).fontSize(block.kind === 'heading' ? 24 - Math.min(block.level, 4) : 16)
            .fontWeight(block.kind === 'heading' ? FontWeight.Bold : FontWeight.Normal)
            .fontFamily(block.kind === 'code' ? 'monospace' : '')
            .fontColor($r('app.color.text_primary')).lineHeight(27).width('100%').copyOption(CopyOptions.LocalDevice)
        }
      }, (block: ReadingBlock): string => block.id)
      if (this.blocks.length > 30) {
        Row({ space: 12 }) {
          SecondaryAction({ label: '上一段', active: this.page > 0, settings: this.settings, onAction: () => { this.page--; } })
          Text(`${this.page + 1} / ${Math.ceil(this.blocks.length / 30)}`).fontColor($r('app.color.text_secondary'))
          SecondaryAction({ label: '下一段', active: (this.page + 1) * 30 < this.blocks.length, settings: this.settings, onAction: () => { this.page++; } })
        }
      }
      if (this.article.sourceUrl) {
        SecondaryAction({ label: '打开原始来源', settings: this.settings, onAction: () => { this.openSource(); } })
        Text(this.article.sourceUrl).fontSize(12).fontColor($r('app.color.text_secondary')).copyOption(CopyOptions.LocalDevice)
      }
      if (this.error) { Text(this.error).fontColor($r('app.color.error')) }
    }.width('100%').alignItems(HorizontalAlign.Start)
  }
}
