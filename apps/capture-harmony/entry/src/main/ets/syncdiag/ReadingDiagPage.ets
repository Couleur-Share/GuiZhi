import { runReadingProbe } from './ReadingProbe';
import { pasteboard } from '@kit.BasicServicesKit';

@Entry
@Component
struct ReadingDiagPage {
  @State report: string = '使用虚构文章与公开测试密钥，不连接服务器。';
  @State busy: boolean = false;

  private async run(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.report = '正在验证系统加密与安全存储…';
    try { this.report = await runReadingProbe(); }
    catch (error) { this.report = '验证失败，可重试：' + (error as Error).message; }
    finally { this.busy = false; }
  }

  private async copy(): Promise<void> {
    try {
      await pasteboard.getSystemPasteboard().setData(pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, this.report));
    } catch { this.report += '\n复制失败，请长按报告复制。'; }
  }

  build() {
    Column({ space: 16 }) {
      Text('阅读同步诊断').fontSize(22)
      Button(this.busy ? '验证中' : '开始验证').enabled(!this.busy).onClick(() => { this.run(); })
      Button('复制报告').enabled(!this.busy).onClick(() => { this.copy(); })
      Scroll() { Text(this.report).fontSize(14).copyOption(CopyOptions.LocalDevice) }.layoutWeight(1)
    }.padding(24).width('100%').height('100%')
  }
}
