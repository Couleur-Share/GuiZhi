import type {
  NetworkProxySettings,
  UpdateChannel,
} from "@guizhi/shared/types";
import type { AIProtocol } from "@guizhi/shared/types";

export const SUPPORTED_LANGUAGES = ["zh", "en"] as const;

export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

export type ThemeMode = "light" | "dark" | "system";
export type AIUsageScenario =
  | "summary"
  | "tagging"
  | "qa"
  | "wiki"
  | "transcription"
  | "ocr";
export type ScenarioModelDefaults = Partial<Record<AIUsageScenario, string>>;
export type AIModelRoute =
  | "mainText"
  | "fastText"
  | "visionText"
  | "embedding"
  | "audioText"
  | "imageGen";
export type ModelRouteDefaults = Partial<Record<AIModelRoute, string>>;

export interface AIModelCapabilities {
  chat?: boolean;
  vision?: boolean;
  reasoning?: boolean;
  toolUse?: boolean;
  webSearch?: boolean;
  embedding?: boolean;
  rerank?: boolean;
  audioTranscription?: boolean;
  imageGeneration?: boolean;
}

export interface ChatModelParams {
  temperature?: number;
  maxTokens?: number;
  topP?: number;
  topK?: number;
  frequencyPenalty?: number;
  presencePenalty?: number;
  stream?: boolean;
  enableThinking?: boolean;
  customParams?: Record<string, string | number | boolean>;
}

export interface AIModelConfig {
  id: string;
  name?: string;
  providerId?: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
  model: string;
  isDefault?: boolean;
  lastVerifiedAt?: string;
  capabilities?: AIModelCapabilities;
  chatParams?: ChatModelParams;
}

export interface AIProviderConfig {
  id: string;
  name?: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
  lastVerifiedAt?: string;
}

export interface SettingsState {
  themeMode: ThemeMode;
  isDarkMode: boolean;
  themeColor: string;
  themeHue: number;
  themeSaturation: number;
  customThemeHex: string;
  settingsUpdatedAt: string;
  fontSize: string;
  backgroundImageEnabled: boolean;
  backgroundImageFileName?: string;
  backgroundImageOpacity: number;
  backgroundImageBlur: number;
  renderMarkdown: boolean;
  editorMarkdownPreview: boolean;
  motionPreference: "off" | "reduced" | "standard";
  autoSave: boolean;
  showLineNumbers: boolean;
  /** Wiki 自动编译开关（ADR 0023：后台把条目增量编译进 LLM 维护的 Wiki），默认关闭 */
  wikiCompileEnabled: boolean;
  launchAtStartup: boolean;
  minimizeOnLaunch: boolean;
  debugMode: boolean;
  closeAction: "ask" | "minimize" | "exit";
  shortcutModes: Record<string, "global" | "local">;
  enableNotifications: boolean;
  showCopyNotification: boolean;
  showSaveNotification: boolean;
  language: SupportedLanguage;
  dataPath: string;
  autoCheckUpdate: boolean;
  useUpdateMirror: boolean;
  updateChannel: UpdateChannel;
  updateChannelExplicitlySet: boolean;
  backupAutoEnabled: boolean;
  backupIntervalHours: number;
  backupKeepCount: number;
  /** yt-dlp 可执行文件路径（空 = 查系统 PATH） */
  ytDlpPath: string;
  /** ffmpeg 可执行文件路径（空 = 托管版 / 系统 PATH） */
  ffmpegPath: string;
  /** 导入在线视频时区分说话人（仅内置本地引擎支持，会让转写慢一倍） */
  transcribeDiarize: boolean;
  aiProvider: string;
  aiApiProtocol: AIProtocol;
  aiApiKey: string;
  aiApiUrl: string;
  aiModel: string;
  aiProviders: AIProviderConfig[];
  aiModels: AIModelConfig[];
  scenarioModelDefaults: ScenarioModelDefaults;
  modelRouteDefaults: ModelRouteDefaults;
  networkProxy: NetworkProxySettings;
  setThemeMode: (mode: ThemeMode) => void;
  setThemeColor: (colorId: string) => void;
  setCustomThemeHex: (hex: string) => void;
  setFontSize: (size: string) => void;
  applyBackgroundImageSelection: (fileName: string) => void;
  setBackgroundImageEnabled: (enabled: boolean) => void;
  setBackgroundImageFileName: (fileName?: string) => void;
  setBackgroundImageOpacity: (opacity: number) => void;
  setBackgroundImageBlur: (blur: number) => void;
  setRenderMarkdown: (enabled: boolean) => void;
  setMotionPreference: (preference: "off" | "reduced" | "standard") => void;
  setEditorMarkdownPreview: (enabled: boolean) => void;
  setAutoSave: (enabled: boolean) => void;
  setShowLineNumbers: (enabled: boolean) => void;
  setWikiCompileEnabled: (enabled: boolean) => void;
  setLaunchAtStartup: (enabled: boolean) => void;
  setMinimizeOnLaunch: (enabled: boolean) => void;
  setDebugMode: (enabled: boolean) => void;
  setEnableNotifications: (enabled: boolean) => void;
  setCloseAction: (action: "ask" | "minimize" | "exit") => void;
  setShortcutMode: (key: string, mode: "global" | "local") => void;
  setShowCopyNotification: (enabled: boolean) => void;
  setShowSaveNotification: (enabled: boolean) => void;
  setLanguage: (lang: string) => void;
  setDataPath: (path: string) => void;
  setAutoCheckUpdate: (enabled: boolean) => void;
  setUseUpdateMirror: (enabled: boolean) => void;
  setUpdateChannel: (channel: UpdateChannel) => void;
  inferUpdateChannel: (version: string) => void;
  setBackupAutoEnabled: (enabled: boolean) => void;
  setBackupIntervalHours: (hours: number) => void;
  setBackupKeepCount: (count: number) => void;
  setYtDlpPath: (path: string) => void;
  setFfmpegPath: (path: string) => void;
  setTranscribeDiarize: (enabled: boolean) => void;
  setAiProvider: (provider: string) => void;
  setAiApiProtocol: (protocol: AIProtocol) => void;
  setAiApiKey: (key: string) => void;
  setAiApiUrl: (url: string) => void;
  setAiModel: (model: string) => void;
  addAiProvider: (config: Omit<AIProviderConfig, "id">) => void;
  updateAiProvider: (id: string, config: Partial<AIProviderConfig>) => void;
  deleteAiProvider: (target: {
    providerId?: string;
    modelIds: string[];
  }) => void;
  addAiModel: (config: Omit<AIModelConfig, "id">) => void;
  updateAiModel: (id: string, config: Partial<AIModelConfig>) => void;
  deleteAiModel: (id: string) => void;
  setDefaultAiModel: (id: string) => void;
  setScenarioModelDefault: (
    scenario: AIUsageScenario,
    modelId: string | null,
  ) => void;
  setModelRouteDefault: (route: AIModelRoute, modelId: string | null) => void;
  applyTheme: () => void;
  setNetworkProxy: (updates: Partial<NetworkProxySettings>) => void;
}

export type SettingsActions = {
  [Key in keyof SettingsState as SettingsState[Key] extends (
    ...args: never[]
  ) => unknown
    ? Key
    : never]: SettingsState[Key];
};

export type SettingsValues = Omit<SettingsState, keyof SettingsActions>;
