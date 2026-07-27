import { useTranslation } from "react-i18next";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import { useSettingsStore } from "../../../stores/settings.store";
import { SettingItem, SettingSection, ToggleSwitch } from "../shared";
import { BinaryEngineRow } from "./BinaryEngineRow";
import { formatEngineVersion } from "./engine-version";
import { FunasrEngineRow } from "./FunasrEngineRow";

/**
 * 采集区：三个可选外部引擎的安装与状态。
 * 主行只留「名称 + 状态 + 主操作」，路径、重新检测、移除都在各行的高级面板里。
 */
export function CaptureSection() {
  const { t } = useTranslation();
  const ytDlpPath = useSettingsStore((state) => state.ytDlpPath);
  const setYtDlpPath = useSettingsStore((state) => state.setYtDlpPath);
  const ffmpegPath = useSettingsStore((state) => state.ffmpegPath);
  const setFfmpegPath = useSettingsStore((state) => state.setFfmpegPath);
  const transcribeDiarize = useSettingsStore(
    (state) => state.transcribeDiarize,
  );
  const setTranscribeDiarize = useSettingsStore(
    (state) => state.setTranscribeDiarize,
  );

  const customPathHint = t(
    "settings.captureCustomPathHint",
    "指定后优先于内置版与系统 PATH；出于安全考虑只能通过「选择文件」设置",
  );

  return (
    <SettingSection title={t("settings.captureSection", "采集")}>
      <BinaryEngineRow
        engineId="ytdlp"
        api={window.api.ytdlp}
        progressChannel={IPC_CHANNELS.YTDLP_DOWNLOAD_PROGRESS}
        customPath={ytDlpPath}
        onCustomPathChange={setYtDlpPath}
        texts={{
          name: t("settings.ytDlpEngine", "yt-dlp 引擎"),
          purpose: t("settings.ytDlpPurpose", "在线视频解析"),
          missingHint: t(
            "settings.ytDlpMissingHint",
            "B 站 / YouTube 等站点的视频导入依赖它",
          ),
          customPathHint,
          customPathPlaceholder: t(
            "settings.captureCustomPathPlaceholder",
            "留空自动选择",
          ),
          installedToast: (version) =>
            t(
              "settings.ytDlpInstallDone",
              "yt-dlp 安装完成（{{version}}），可在导入任务中重试解析",
              { version: formatEngineVersion(version) },
            ),
          removedToast: t("settings.ytDlpRemoved", "已移除内置版 yt-dlp"),
          removeConfirmMessage: t(
            "settings.ytDlpRemoveConfirm",
            "确定移除内置版 yt-dlp？移除后在线视频解析将回退到系统 PATH，之后可随时重新一键安装。",
          ),
        }}
      />

      <BinaryEngineRow
        engineId="ffmpeg"
        api={window.api.ffmpeg}
        progressChannel={IPC_CHANNELS.FFMPEG_DOWNLOAD_PROGRESS}
        customPath={ffmpegPath}
        onCustomPathChange={setFfmpegPath}
        optional
        texts={{
          name: t("settings.ffmpegEngine", "ffmpeg 引擎"),
          purpose: t("settings.ffmpegPurpose", "转写前音频转码"),
          missingHint: t(
            "settings.ffmpegMissingHint",
            "装了会先转成 16kHz 单声道，上传更小更兼容",
          ),
          customPathHint,
          customPathPlaceholder: t(
            "settings.captureCustomPathPlaceholder",
            "留空自动选择",
          ),
          installedToast: (version) =>
            t(
              "settings.ffmpegInstallDone",
              "ffmpeg 安装完成（{{version}}），之后转写会自动压缩音频",
              { version: formatEngineVersion(version) },
            ),
          removedToast: t("settings.ffmpegRemoved", "已移除内置版 ffmpeg"),
          removeConfirmMessage: t(
            "settings.ffmpegRemoveConfirm",
            "确定移除内置版 ffmpeg？移除后转写将直传原始音频，之后可随时重新一键安装。",
          ),
        }}
      />

      <FunasrEngineRow />

      <SettingItem
        label={t("settings.transcribeDiarize", "导入时区分说话人")}
        description={t(
          "settings.transcribeDiarizeDesc",
          "访谈、会议这类多人内容才有意义：转写会慢一倍，且只有内置本地引擎支持。单人内容开着没有收益。",
        )}
      >
        <ToggleSwitch
          ariaLabel={t("settings.transcribeDiarize", "导入时区分说话人")}
          checked={transcribeDiarize}
          onChange={setTranscribeDiarize}
        />
      </SettingItem>
    </SettingSection>
  );
}
