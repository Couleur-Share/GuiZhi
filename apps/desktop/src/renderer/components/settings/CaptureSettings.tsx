import { CaptureSection } from "./capture/CaptureSection";
import { WebCaptureSettings } from "./WebCaptureSettings";

/** 平台账号、采集工具与转写引擎的独立设置分区。 */
export function CaptureSettings() {
  return <div className="space-y-6"><WebCaptureSettings /><CaptureSection /></div>;
}
