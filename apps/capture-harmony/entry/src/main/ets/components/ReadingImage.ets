import { image } from '@kit.ImageKit';
import { ReadingLibrary } from '../core/ReadingLibrary';

@Component
export struct ReadingImage {
  @Prop objectId: string = '';
  @Prop label: string = '图片';
  @Prop @Watch('load') revision: number = 0;
  library: ReadingLibrary | null = null;
  @State pixels: image.PixelMap | undefined = undefined;
  @State message: string = '尚未保存图片，可点“离线保存图片”下载';
  private epoch: number = 0;
  aboutToAppear(): void { this.load(); }
  aboutToDisappear(): void { this.epoch++; this.release(); }
  private release(): void { if (this.pixels) { this.pixels.release(); this.pixels = undefined; } }
  private async load(): Promise<void> {
    const epoch = ++this.epoch; this.release();
    if (!this.library) return;
    let bytes: Uint8Array | null = null, source: image.ImageSource | undefined;
    try {
      bytes = await this.library.image(this.objectId);
      if (!bytes || epoch !== this.epoch) return;
      source = image.createImageSource(bytes.buffer);
      const info = await source.getImageInfo();
      if (info.size.width < 1 || info.size.height < 1 || info.size.width * info.size.height > 16000000) throw new Error('图片尺寸超过安全阅读上限');
      const scale = Math.min(1, 768 / info.size.width, 1024 / info.size.height);
      const pixels = await source.createPixelMap({ desiredSize: { width: Math.max(1, Math.floor(info.size.width * scale)), height: Math.max(1, Math.floor(info.size.height * scale)) } });
      if (epoch !== this.epoch) { await pixels.release(); return; }
      this.pixels = pixels; this.message = '';
    } catch (error) { if (epoch === this.epoch) this.message = `图片未就绪：${(error as Error).message}`; }
    finally { if (source) await source.release(); if (bytes) bytes.fill(0); }
  }
  build() {
    Column({ space: 8 }) {
      if (this.pixels) { Image(this.pixels).width('100%').constraintSize({ maxHeight: 420 }).objectFit(ImageFit.Contain).accessibilityText(this.label) }
      else { Text(this.message).fontSize(13).fontColor($r('app.color.text_secondary')).copyOption(CopyOptions.LocalDevice) }
      if (this.label !== '图片') { Text(this.label).fontSize(12).fontColor($r('app.color.text_secondary')) }
    }.width('100%').padding(12).borderRadius(12).backgroundColor($r('app.color.surface_muted'))
  }
}
