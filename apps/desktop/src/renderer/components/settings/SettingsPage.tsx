import {
  lazy,
  Suspense,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import {
  SettingsIcon,
  PaletteIcon,
  DatabaseIcon,
  InfoIcon,
  ArrowLeftIcon,
  BrainIcon,
  ImageIcon,
  KeyboardIcon,
  PlugIcon,
  WifiIcon,
  RadioTowerIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUIStore, type SettingsSectionId } from "../../stores/ui.store";
// 直接指向文件而不走 ui 的桶文件：桶文件会把 ModelIcons 连同二十来个
// provider SVG 一起拖进设置页的首屏依赖，dev 下每个都是一次单独请求
import { Spinner } from "../ui/Spinner";

interface SettingsPageProps {
  onBack: () => void;
}

// Settings menu items - use i18n keys instead of hardcoded text
// 设置菜单项 - 使用 key 而非硬编码文本
const SETTINGS_MENU = [
  { id: "general", labelKey: "settings.general", icon: SettingsIcon },
  { id: "appearance", labelKey: "settings.appearance", icon: PaletteIcon },
  { id: "data", labelKey: "settings.data", icon: DatabaseIcon },
  { id: "network", labelKey: "settings.network", icon: WifiIcon },
  { id: "capture", labelKey: "settings.captureAndTranscription", icon: RadioTowerIcon },
  { id: "ai", labelKey: "settings.ai", icon: BrainIcon },
  { id: "illustration", labelKey: "settings.illustration", icon: ImageIcon },
  // 紧跟模型服务与正文配图：找 MCP 时的心理模型是「跟 AI 有关的设置」，
  // 而它与「数据」那边「把内容搬出去」是两回事
  { id: "mcp", labelKey: "settings.mcp", icon: PlugIcon },
  { id: "shortcuts", labelKey: "settings.shortcuts", icon: KeyboardIcon },
  { id: "about", labelKey: "settings.about", icon: InfoIcon },
];

const loadGeneralSettings = () => import("./GeneralSettings");
const loadAppearanceSettings = () => import("./AppearanceSettings");
const loadLanguageSettings = () => import("./LanguageSettings");
const loadShortcutsSettings = () => import("./ShortcutsSettings");
const loadAboutSettings = () => import("./AboutSettings");
const loadDataSettings = () => import("./DataSettings");
const loadNetworkSettings = () => import("./NetworkSettings");
const loadCaptureSettings = () => import("./CaptureSettings");
const loadAISettings = () => import("./AISettingsPrototype");
const loadIllustrationSettings = () => import("./IllustrationSettings");
const loadMcpSettings = () => import("./mcp/McpSettings");

const GeneralSettings = lazy(() =>
  loadGeneralSettings().then((module) => ({
    default: module.GeneralSettings,
  })),
);
const AppearanceSettings = lazy(() =>
  loadAppearanceSettings().then((module) => ({
    default: module.AppearanceSettings,
  })),
);
const LanguageSettings = lazy(() =>
  loadLanguageSettings().then((module) => ({
    default: module.LanguageSettings,
  })),
);
const ShortcutsSettings = lazy(() =>
  loadShortcutsSettings().then((module) => ({
    default: module.ShortcutsSettings,
  })),
);
const AboutSettings = lazy(() =>
  loadAboutSettings().then((module) => ({
    default: module.AboutSettings,
  })),
);
const DataSettings = lazy(() =>
  loadDataSettings().then((module) => ({
    default: module.DataSettings,
  })),
);
const NetworkSettings = lazy(() =>
  loadNetworkSettings().then((module) => ({
    default: module.NetworkSettings,
  })),
);
const CaptureSettings = lazy(() =>
  loadCaptureSettings().then((module) => ({ default: module.CaptureSettings })),
);
const AISettingsPrototype = lazy(() =>
  loadAISettings().then((module) => ({
    default: module.AISettingsPrototype,
  })),
);
const IllustrationSettings = lazy(() =>
  loadIllustrationSettings().then((module) => ({
    default: module.IllustrationSettings,
  })),
);
const McpSettings = lazy(() =>
  loadMcpSettings().then((module) => ({
    default: module.McpSettings,
  })),
);

/**
 * 应用启动后的空闲时段预热设置页及其所有分区。
 * 同一动态 import 会复用 ESM 模块缓存，首次点击时不会再触发 Suspense 加载态。
 */
export function prefetchSettingsSections(): void {
  for (const loadSection of [
    loadGeneralSettings,
    loadAppearanceSettings,
    loadLanguageSettings,
    loadShortcutsSettings,
    loadAboutSettings,
    loadDataSettings,
    loadNetworkSettings,
    loadCaptureSettings,
    loadAISettings,
    loadIllustrationSettings,
    loadMcpSettings,
  ]) {
    void loadSection().catch(() => {});
  }
}

function SettingsContentFallback({ label }: { label: string }) {
  return (
    <div className="flex min-h-64 items-center justify-center">
      <Spinner size="lg" tone="muted" label={label} />
    </div>
  );
}

export function SettingsPage({ onBack }: SettingsPageProps) {
  const pendingSettingsSection = useUIStore(
    (state) => state.pendingSettingsSection,
  );
  const consumeSettingsSectionRequest = useUIStore(
    (state) => state.consumeSettingsSectionRequest,
  );
  const [activeSection, setActiveSection] =
    useState<SettingsSectionId>("general");
  const contentScrollRef = useRef<HTMLDivElement>(null);
  const { t } = useTranslation();

  useEffect(() => {
    if (!pendingSettingsSection) {
      return;
    }

    const requestedSection = consumeSettingsSectionRequest();
    if (
      requestedSection &&
      SETTINGS_MENU.some((item) => item.id === requestedSection)
    ) {
      setActiveSection(requestedSection);
    }
  }, [consumeSettingsSectionRequest, pendingSettingsSection]);

  // 内容区是设置页共用的滚动容器；切换分区时必须归零，避免把上一页的
  // scrollTop 带到内容长度不同的下一页。
  useLayoutEffect(() => {
    contentScrollRef.current?.scrollTo({ top: 0 });
  }, [activeSection]);

  const renderContent = () => {
    switch (activeSection) {
      case "general":
        return <GeneralSettings />;
      case "appearance":
        return <AppearanceSettings />;
      case "data":
        return <DataSettings />;
      case "network":
        return <NetworkSettings />;
      case "capture":
        return <CaptureSettings />;
      case "ai":
        return <AISettingsPrototype />;
      case "illustration":
        return <IllustrationSettings />;
      case "mcp":
        return <McpSettings />;
      case "language":
        return <LanguageSettings />;
      case "shortcuts":
        return <ShortcutsSettings />;
      case "about":
        return <AboutSettings />;
    }
  };

  return (
    <div className="flex flex-1 overflow-hidden">
      {/* 设置侧边栏 */}
      <div className="w-56 app-wallpaper-panel border-r border-border flex flex-col">
        {/* 返回按钮 */}
        <div className="p-3 border-b border-border">
          <button
            type="button"
            onClick={onBack}
            className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ArrowLeftIcon className="w-4 h-4" aria-hidden="true" />
            <span>{t("common.back")}</span>
          </button>
        </div>

        {/* 菜单列表 */}
        <nav className="flex-1 overflow-y-auto p-2 space-y-1">
          {SETTINGS_MENU.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setActiveSection(item.id as SettingsSectionId)}
              data-testid={`settings-nav-${item.id}`}
              aria-pressed={activeSection === item.id}
              className={`w-full flex items-center gap-3 px-3 py-2 rounded-lg text-[13px] transition-all duration-quick ${
                activeSection === item.id
                  ? "bg-primary text-primary-foreground shadow-sm"
                  : "text-foreground/80 hover:bg-muted/70"
              }`}
            >
              <item.icon className="w-4 h-4" aria-hidden="true" />
              <span>{t(item.labelKey)}</span>
            </button>
          ))}
        </nav>
      </div>

      {/* 设置内容区 - 自适应宽度 */}
      <div
        ref={contentScrollRef}
        className={
          activeSection === "ai"
            ? "flex-1 overflow-hidden app-wallpaper-section"
            : "flex-1 overflow-y-auto px-5 py-5 app-wallpaper-section sm:px-6 xl:px-8 2xl:px-10"
        }
      >
        <div
          data-testid="settings-content-shell"
          className={
            activeSection === "ai"
              ? "h-full max-w-none"
              : // 用 px 钉死上限：html 的 font-size 随「字体大小」变，rem 制的
                // max-w-5xl 在「小」时一起缩窄，右边会空出一块；断点仍是视口
                // px，不受字号影响，所以只把上限改成中档（16px）下的等效像素。
                "w-full max-w-[1024px] xl:max-w-[1152px] 2xl:max-w-[1280px]"
          }
        >
          {activeSection === "ai" ? null : (
            <h1 className="text-lg font-semibold mb-4">
              {t(
                SETTINGS_MENU.find((m) => m.id === activeSection)?.labelKey ||
                  "",
              )}
            </h1>
          )}
          <div
            key={activeSection}
            className={
              activeSection === "ai"
                ? "h-full animate-in fade-in slide-in-from-bottom-2 duration-base"
                : "animate-in fade-in slide-in-from-bottom-2 duration-base"
            }
          >
            <Suspense
              fallback={
                <SettingsContentFallback
                  label={t("common.loading", "Loading...")}
                />
              }
            >
              {renderContent()}
            </Suspense>
          </div>
        </div>
      </div>
    </div>
  );
}
