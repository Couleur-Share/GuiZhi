// Package settings 是设置的类型化模型：字段、默认值、归一化规则，以及旧版（React 渲染层）localStorage 的解析。
// 纯逻辑：不碰文件、数据库与窗口，所以 go test ./native/... 能直接覆盖；存储由 package main 的适配器提供。
//
// 单一真相源 = 每个字段只有一个物理存放处，对界面只有一份快照（Values）：
//   - UI：界面自有的偏好（外观、阅读/编辑、关闭行为、更新）→ config/ui-prefs.json 的 settings 段；
//   - App：Go 侧其他组件也要读的设置（备份调度、代理、工具路径、开机自启……）→ SQLite settings 表，
//     沿用现有的 settings:get / settings:set 处理器与校验，不另起一份；
//   - 不在这里的：AI 连接（供应商、模型、密钥、路由）在 config/ai-models.json，快捷键与模式在
//     config/shortcuts.json、shortcut-mode.json，它们各有自己的服务；界面偏好里零散的键值（窗格宽度、
//     列配置、阅读位置……）见 prefs.go，经 Prefs 接口存取。
//
// 字段名（JSON 键）沿用渲染层 guizhi-settings 的名字，旧数据因此能直接按名字导入。
//
// Package settings is the typed settings model of the native UI: fields, defaults, normalization,
// and parsing of the old renderer's localStorage. Pure logic; storage lives in package main.
package settings

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// 取值集合。取值字符串与渲染层一致，旧数据可原样导入。
type (
	// ThemeMode 是明暗偏好；system 跟随系统。
	ThemeMode string
	// MotionPreference 是动效偏好；system 遵从系统「减少动态效果」，standard 始终播放完整动画。
	MotionPreference string
	// FontSize 是字号档。
	FontSize string
	// CloseAction 是点关闭按钮时的行为。
	CloseAction string
	// Language 是界面语言。
	Language string
	// UpdateChannel 是更新通道。
	UpdateChannel string
)

const (
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
	ThemeSystem ThemeMode = "system"

	MotionSystem   MotionPreference = "system"
	MotionOff      MotionPreference = "off"
	MotionReduced  MotionPreference = "reduced"
	MotionStandard MotionPreference = "standard"

	FontSmall  FontSize = "small"
	FontMedium FontSize = "medium"
	FontLarge  FontSize = "large"

	CloseAsk      CloseAction = "ask"
	CloseMinimize CloseAction = "minimize"
	CloseExit     CloseAction = "exit"

	LangZh Language = "zh"
	LangEn Language = "en"

	ChannelStable  UpdateChannel = "stable"
	ChannelPreview UpdateChannel = "preview"
)

// 默认值里需要被别处引用的几个。
const (
	DefaultThemeColor      = "royal-blue"
	DefaultThemeHue        = 220
	DefaultThemeSaturation = 70
	DefaultCustomThemeHex  = "#3b82f6"

	// 壁纸默认不透明度 1、模糊 0：与渲染层 DEFAULT_BACKGROUND_IMAGE_* 一致（早期版本的默认值 0.22 / 14 已废弃）。
	DefaultBackgroundOpacity = 1.0
	DefaultBackgroundBlur    = 0.0

	MaxBackgroundBlur = 50.0

	DefaultBackupIntervalHours = 24
	MinBackupIntervalHours     = 1
	MaxBackupIntervalHours     = 168
	DefaultBackupKeepCount     = 10
	MinBackupKeepCount         = 1
	MaxBackupKeepCount         = 100
)

// AIScenarios 是「按使用场景指定模型」的场景集合（与渲染层 AIUsageScenario 一致）。
var AIScenarios = []string{"summary", "tagging", "qa", "wiki", "research", "transcription", "ocr"}

// UI 是界面自有的设置，存放在 config/ui-prefs.json 的 settings 段。
type UI struct {
	ThemeMode       ThemeMode `json:"themeMode"`
	ThemeColor      string    `json:"themeColor"` // 预设 ID，或 custom
	ThemeHue        float64   `json:"themeHue"`
	ThemeSaturation float64   `json:"themeSaturation"`
	CustomThemeHex  string    `json:"customThemeHex"`
	FontSize        FontSize  `json:"fontSize"`

	BackgroundImageEnabled  bool    `json:"backgroundImageEnabled"`
	BackgroundImageFileName string  `json:"backgroundImageFileName"` // 空串表示没有壁纸；是图片目录下的文件名，不是路径
	BackgroundImageOpacity  float64 `json:"backgroundImageOpacity"`
	BackgroundImageBlur     float64 `json:"backgroundImageBlur"`

	MotionPreference MotionPreference `json:"motionPreference"`

	RenderMarkdown        bool `json:"renderMarkdown"`
	EditorMarkdownPreview bool `json:"editorMarkdownPreview"`
	AutoSave              bool `json:"autoSave"`
	ShowLineNumbers       bool `json:"showLineNumbers"`
	WikiCompileEnabled    bool `json:"wikiCompileEnabled"`

	DebugMode   bool        `json:"debugMode"`
	CloseAction CloseAction `json:"closeAction"`

	AutoCheckUpdate            bool          `json:"autoCheckUpdate"`
	UseUpdateMirror            bool          `json:"useUpdateMirror"`
	UpdateChannel              UpdateChannel `json:"updateChannel"`
	UpdateChannelExplicitlySet bool          `json:"updateChannelExplicitlySet"`

	// ScenarioModelDefaults 是「场景 → 模型 ID」。只存 ID，不含密钥；模型本身在 ai-models.json。
	ScenarioModelDefaults map[string]string `json:"scenarioModelDefaults"`
}

// App 是 Go 侧其他组件也要读的设置，存放在 SQLite settings 表（键名与表里一致）。
type App struct {
	Language Language `json:"language"`

	LaunchAtStartup        bool `json:"launchAtStartup"`
	MinimizeOnLaunch       bool `json:"minimizeOnLaunch"`
	BackgroundTasksEnabled bool `json:"backgroundTasksEnabled"`
	EnableNotifications    bool `json:"enableNotifications"`

	NetworkProxy NetworkProxy `json:"networkProxy"`
	FlareSolverr FlareSolverr `json:"flareSolverr"`

	BackupAutoEnabled   bool `json:"backupAutoEnabled"`
	BackupIntervalHours int  `json:"backupIntervalHours"`
	BackupKeepCount     int  `json:"backupKeepCount"`

	// YtDlpPath、FfmpegPath 是会被外壳 spawn 的可执行文件：非空值只接受文件选择器返回过的路径（settings.go 把关）。
	YtDlpPath         string `json:"ytDlpPath"`
	FfmpegPath        string `json:"ffmpegPath"`
	TranscribeDiarize bool   `json:"transcribeDiarize"`
}

// NetworkProxy 是网络代理设置；规则（模式、协议、端口范围、绕过列表）由 Go 侧 normalizeNetworkProxy 把关。
type NetworkProxy struct {
	Mode     string `json:"mode"`     // system | direct | manual
	Protocol string `json:"protocol"` // http | https | socks5
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Bypass   string `json:"bypass"`
}

// String 给日志与 %v 用：口令不进日志。
func (p NetworkProxy) String() string {
	pw := ""
	if p.Password != "" {
		pw = "***"
	}
	return fmt.Sprintf("{mode=%s protocol=%s host=%s port=%d username=%s password=%s bypass=%s}",
		p.Mode, p.Protocol, p.Host, p.Port, p.Username, pw, p.Bypass)
}

// GoString 让 %#v 同样不泄露口令。
func (p NetworkProxy) GoString() string { return "settings.NetworkProxy" + p.String() }

// FlareSolverr 是 FlareSolverr 采集辅助的设置；规则由 Go 侧 validateFlareSolverr 把关。
type FlareSolverr struct {
	Enabled    bool   `json:"enabled"`
	Connection string `json:"connection"` // local | ssh
	Port       int    `json:"port"`
	SSHHost    string `json:"sshHost"`
}

// Values 是全部设置的一份快照。
type Values struct {
	UI  UI
	App App
}

// DefaultProxy 是网络代理的默认值（与 packages/shared 的 DEFAULT_NETWORK_PROXY_SETTINGS 一致）。
func DefaultProxy() NetworkProxy {
	return NetworkProxy{Mode: "system", Protocol: "http", Port: 7890, Bypass: "<local>,localhost,127.0.0.1,::1"}
}

// DefaultFlareSolverr 是 FlareSolverr 的默认值（与 DEFAULT_FLARESOLVERR_SETTINGS 一致）。
func DefaultFlareSolverr() FlareSolverr {
	return FlareSolverr{Connection: "local", Port: 8191}
}

// DefaultUI 是界面设置的默认值（与渲染层 createDefaultSettingsValues 一致）。
func DefaultUI() UI {
	return UI{
		ThemeMode: ThemeSystem, ThemeColor: DefaultThemeColor, ThemeHue: DefaultThemeHue, ThemeSaturation: DefaultThemeSaturation,
		CustomThemeHex: DefaultCustomThemeHex, FontSize: FontMedium,
		BackgroundImageEnabled: true, BackgroundImageOpacity: DefaultBackgroundOpacity, BackgroundImageBlur: DefaultBackgroundBlur,
		MotionPreference: MotionSystem,
		RenderMarkdown:   true, EditorMarkdownPreview: true, AutoSave: true,
		CloseAction:     CloseAsk,
		AutoCheckUpdate: true, UpdateChannel: ChannelStable,
		ScenarioModelDefaults: map[string]string{},
	}
}

// DefaultApp 是 Go 侧设置的默认值。语言默认中文（与 Electron 版 DEFAULT_SETTINGS 一致）；
// 首次运行时界面语言怎么选（系统语言匹配）是外壳的事，选定后经 Update 写入。
func DefaultApp() App {
	return App{
		Language:            LangZh,
		EnableNotifications: true,
		NetworkProxy:        DefaultProxy(), FlareSolverr: DefaultFlareSolverr(),
		BackupAutoEnabled: true, BackupIntervalHours: DefaultBackupIntervalHours, BackupKeepCount: DefaultBackupKeepCount,
	}
}

// Default 是全新安装的设置。
func Default() Values { return Values{UI: DefaultUI(), App: DefaultApp()} }

// Clone 返回深拷贝（ScenarioModelDefaults 是 map，不能共用）。
func (v Values) Clone() Values {
	v.UI = v.UI.Clone()
	return v
}

// Clone 返回深拷贝。
func (u UI) Clone() UI {
	m := make(map[string]string, len(u.ScenarioModelDefaults))
	for k, val := range u.ScenarioModelDefaults {
		m[k] = val
	}
	u.ScenarioModelDefaults = m
	return u
}

// Equal 按值比较（nil map 与空 map 相等）。
func (u UI) Equal(o UI) bool {
	if len(u.ScenarioModelDefaults) == 0 && len(o.ScenarioModelDefaults) == 0 {
		u.ScenarioModelDefaults, o.ScenarioModelDefaults = nil, nil
	}
	return reflect.DeepEqual(u, o)
}

// Equal 按值比较。
func (a App) Equal(o App) bool { return a == o }

// Equal 按值比较。
func (v Values) Equal(o Values) bool { return v.UI.Equal(o.UI) && v.App.Equal(o.App) }

// Keys 把结构体编码成「JSON 键 → 原始值」，用于逐键比较与按键写入。
func Keys(v any) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ChangedKeys 返回 next 相对 prev 值不同的 JSON 键（逐键比较编码后的值）。
func ChangedKeys(prev, next any) (map[string]json.RawMessage, error) {
	a, err := Keys(prev)
	if err != nil {
		return nil, err
	}
	b, err := Keys(next)
	if err != nil {
		return nil, err
	}
	changed := map[string]json.RawMessage{}
	for k, nv := range b {
		if ov, ok := a[k]; !ok || string(ov) != string(nv) {
			changed[k] = nv
		}
	}
	return changed, nil
}
