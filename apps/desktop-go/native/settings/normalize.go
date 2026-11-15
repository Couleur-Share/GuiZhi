package settings

import (
	"encoding/json"
	"math"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 归一化规则移植自渲染层 stores/settings（settings-persistence.ts 的 normalizeMergedState、
// settings-appearance.ts、settings-normalizers.ts、settings-ai.ts 的场景默认值）。
// testdata/golden-settings.json 由 TS 实现导出，settings 的 golden 测试逐项核对两边一致；
// 与 TS 有意不同的地方都在下面标了「额外」，并有各自的测试。

// isJSSpace 是 JS String.prototype.trim 认的空白（含 BOM 与 Unicode 空格，不含 U+0085）。
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

// toFixed1 对应 Number(x.toFixed(1))：按 x 的精确值四舍五入到一位小数，平局取较大者（ECMAScript 规范）。
// 不用 strconv：它在精确平局（如 0.25）时取偶，与 JS 不同；也不能先乘 10 再取整——0.15*10 在浮点里恰好是 1.5。
func toFixed1(x float64) float64 {
	r := new(big.Rat).SetFloat64(x)
	if r == nil {
		return x
	}
	r.Mul(r, big.NewRat(10, 1))
	r.Add(r, big.NewRat(1, 2))
	// floor(r)：big.Rat 的 Num/Denom 做整数除法向零取整，负数要再减一
	n := new(big.Int).Quo(r.Num(), r.Denom())
	if r.Sign() < 0 && new(big.Int).Rem(r.Num(), r.Denom()).Sign() != 0 {
		n.Sub(n, big.NewInt(1))
	}
	f, _ := new(big.Rat).SetFrac(n, big.NewInt(10)).Float64()
	return f
}

// ClampOpacity 对应 clampBackgroundImageOpacity：非有限数取默认值，其余夹到 0–1。
func ClampOpacity(x float64) float64 {
	if !finite(x) {
		return DefaultBackgroundOpacity
	}
	return clamp(x, 0, 1)
}

// ClampBlur 对应 clampBackgroundImageBlur：非有限数取默认值，其余夹到 0–50 并保留一位小数。
func ClampBlur(x float64) float64 {
	if !finite(x) {
		return DefaultBackgroundBlur
	}
	return toFixed1(clamp(x, 0, MaxBackgroundBlur))
}

const localImagePrefix = "local-image://"

var remoteImageScheme = regexp.MustCompile(`(?i)^(https?:|data:|blob:)`)

// decodeURIComponentOrSelf 对应 try { decodeURIComponent(s) } catch { return s }。
func decodeURIComponentOrSelf(s string) string {
	out, err := url.PathUnescape(s)
	if err != nil || !utf8.ValidString(out) {
		return s // JS 在百分号序列非法或解出非法 UTF-8 时抛 URIError
	}
	return out
}

// NormalizeBackgroundFileName 对应 normalizeBackgroundImageFileName：壁纸只能是图片目录下的一个文件名，
// 拒绝远程地址、路径分隔符、.. 与控制字符；不合法返回空串（渲染层返回 undefined）。
func NormalizeBackgroundFileName(value string) string {
	trimmed := jsTrim(value)
	if trimmed == "" {
		return ""
	}
	name := trimmed
	if strings.HasPrefix(trimmed, localImagePrefix) {
		name = decodeURIComponentOrSelf(trimmed[len(localImagePrefix):])
	}
	if name == "" || remoteImageScheme.MatchString(name) || strings.Contains(name, "..") ||
		strings.ContainsAny(name, "\x00/\\?#") {
		return ""
	}
	return name
}

// NormalizeLanguage 对应 normalizeLanguage：zh/en 原样，zh-* 归 zh，其余归 en。
func NormalizeLanguage(lang string) Language {
	if lang == string(LangZh) || lang == string(LangEn) {
		return Language(lang)
	}
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return LangZh
	}
	return LangEn
}

// ClampBackup 对应渲染层 clampBackupNumber：四舍五入后夹到 [lo, hi]；非有限数取 fallback。
func ClampBackup(x float64, fallback, lo, hi int) int {
	if !finite(x) {
		return fallback
	}
	r := math.Floor(x + 0.5) // Math.round：平局向正无穷
	return int(math.Min(math.Max(r, float64(lo)), float64(hi)))
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// NormalizeScenarioDefaults 对应 normalizeScenarioModelDefaults：只留已知场景，模型 ID 去空白后非空。
func NormalizeScenarioDefaults(in map[string]string) map[string]string {
	out := map[string]string{}
	for _, scenario := range AIScenarios {
		if id := jsTrim(in[scenario]); id != "" {
			out[scenario] = id
		}
	}
	return out
}

// Normalize 把界面设置收敛到合法取值。
func (u *UI) Normalize() {
	switch u.ThemeMode {
	case ThemeLight, ThemeDark, ThemeSystem:
	default:
		u.ThemeMode = ThemeSystem
	}
	switch u.FontSize {
	case FontSmall, FontMedium, FontLarge:
	default:
		u.FontSize = FontMedium
	}
	switch u.MotionPreference {
	case MotionSystem, MotionOff, MotionReduced, MotionStandard:
	default:
		u.MotionPreference = MotionSystem
	}
	switch u.CloseAction {
	case CloseAsk, CloseMinimize, CloseExit:
	default:
		u.CloseAction = CloseAsk
	}
	switch u.UpdateChannel {
	case ChannelStable, ChannelPreview:
	default:
		u.UpdateChannel = ChannelStable
	}
	u.BackgroundImageFileName = NormalizeBackgroundFileName(u.BackgroundImageFileName)
	u.BackgroundImageOpacity = ClampOpacity(u.BackgroundImageOpacity)
	u.BackgroundImageBlur = ClampBlur(u.BackgroundImageBlur)
	// Markdown 渲染视图是默认行为：该设置没有界面入口，历史持久化里的 false 都是旧默认值残留，一律翻转
	// （加设置界面时移除此强制，与渲染层 normalizeMergedPresentationSettings 一致）
	u.EditorMarkdownPreview = true

	// 额外：渲染层不夹取色相与饱和度、不校验自定义色，坏值进了 CSS 只是颜色怪；
	// 原生外观装配需要合法范围，所以这里收敛，避免坏值一路传到绘制。
	if !finite(u.ThemeHue) {
		u.ThemeHue = DefaultThemeHue
	}
	if !finite(u.ThemeSaturation) {
		u.ThemeSaturation = DefaultThemeSaturation
	}
	u.ThemeHue = clamp(u.ThemeHue, 0, 360)
	u.ThemeSaturation = clamp(u.ThemeSaturation, 0, 100)
	if !hexColor.MatchString(u.CustomThemeHex) {
		u.CustomThemeHex = DefaultCustomThemeHex
	}
	if u.ThemeColor == "" {
		u.ThemeColor = DefaultThemeColor
	}
	u.ScenarioModelDefaults = NormalizeScenarioDefaults(u.ScenarioModelDefaults)
}

// Normalize 把 Go 侧设置收敛到合法取值。网络代理与 FlareSolverr 的规则由写入处（settings.go）把关，这里不重复。
func (a *App) Normalize() {
	a.Language = NormalizeLanguage(string(a.Language))
	// 额外：渲染层只在设置器里夹取备份数字，持久化的坏值原样留着，由调度器读取时再夹；这里在边界一次收敛。
	a.BackupIntervalHours = ClampBackup(float64(a.BackupIntervalHours), DefaultBackupIntervalHours, MinBackupIntervalHours, MaxBackupIntervalHours)
	a.BackupKeepCount = ClampBackup(float64(a.BackupKeepCount), DefaultBackupKeepCount, MinBackupKeepCount, MaxBackupKeepCount)
}

// Normalize 收敛全部设置。
func (v *Values) Normalize() {
	v.UI.Normalize()
	v.App.Normalize()
}

// ── 宽松解码 ──

// fields 是「键 → 原始 JSON」，按键取带类型的值；类型不对等同于没有。
type fields map[string]json.RawMessage

// rawString 取 JSON 字符串的值；null 或其他类型返回 false。
func rawString(raw json.RawMessage) (string, bool) {
	var s string
	if isJSONString(raw) && json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

func (f fields) str(key string) (string, bool) {
	raw, ok := f[key]
	if !ok {
		return "", false
	}
	return rawString(raw)
}

func (f fields) boolean(key string) (bool, bool) {
	var b bool
	if raw, ok := f[key]; ok && json.Unmarshal(raw, &b) == nil && (string(raw) == "true" || string(raw) == "false") {
		return b, true
	}
	return false, false
}

func (f fields) number(key string) (float64, bool) {
	var n float64
	if raw, ok := f[key]; ok && json.Unmarshal(raw, &n) == nil && finite(n) && isJSONNumber(raw) {
		return n, true
	}
	return 0, false
}

func (f fields) object(key string) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if raw, ok := f[key]; ok && json.Unmarshal(raw, &m) == nil && m != nil {
		return m, true
	}
	return nil, false
}

func isJSONString(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '"'
}

func isJSONNumber(raw json.RawMessage) bool {
	return len(raw) > 0 && (raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9'))
}

// PersistedVersion 是渲染层 guizhi-settings 持久化的当前版本（v3）。ui-prefs.json 的 settings 段
// 总是这个版本的形状（写入前已归一化），所以读它时按当前版本解码，不再套用迁移。
const PersistedVersion = 3

// DecodeUI 从旧 localStorage 的 state（或文件的 settings 段）解出界面设置。
//
// 对应渲染层的「merge(migrate(persisted), defaults)」：以默认值为底，只取已知键（已下线的同步字段——含明文凭据的
// webdavPassword、s3SecretAccessKey——因此不会被带进来），类型不对的字段退回默认，最后归一化。
// persistedVersion 小于 3 时套用 v3 迁移：此前「标准」动效是默认值、无法与主动选择区分，改回跟随系统。
func DecodeUI(raw map[string]json.RawMessage, persistedVersion int) UI {
	u := DefaultUI()
	f := fields(raw)
	if s, ok := f.str("themeMode"); ok {
		u.ThemeMode = ThemeMode(s)
	}
	if s, ok := f.str("themeColor"); ok {
		u.ThemeColor = s
	}
	if n, ok := f.number("themeHue"); ok {
		u.ThemeHue = n
	}
	if n, ok := f.number("themeSaturation"); ok {
		u.ThemeSaturation = n
	}
	if s, ok := f.str("customThemeHex"); ok {
		u.CustomThemeHex = s
	}
	if s, ok := f.str("fontSize"); ok {
		u.FontSize = FontSize(s)
	}
	if b, ok := f.boolean("backgroundImageEnabled"); ok {
		u.BackgroundImageEnabled = b
	}
	if s, ok := f.str("backgroundImageFileName"); ok {
		u.BackgroundImageFileName = s
	}
	if n, ok := f.number("backgroundImageOpacity"); ok {
		u.BackgroundImageOpacity = n
	}
	if n, ok := f.number("backgroundImageBlur"); ok {
		u.BackgroundImageBlur = n
	}
	if s, ok := f.str("motionPreference"); ok {
		u.MotionPreference = MotionPreference(s)
	}
	for key, dst := range map[string]*bool{
		"renderMarkdown": &u.RenderMarkdown, "autoSave": &u.AutoSave, "showLineNumbers": &u.ShowLineNumbers,
		"wikiCompileEnabled": &u.WikiCompileEnabled, "debugMode": &u.DebugMode,
		"autoCheckUpdate": &u.AutoCheckUpdate, "useUpdateMirror": &u.UseUpdateMirror,
		"updateChannelExplicitlySet": &u.UpdateChannelExplicitlySet,
	} {
		if b, ok := f.boolean(key); ok {
			*dst = b
		}
	}
	if s, ok := f.str("closeAction"); ok {
		u.CloseAction = CloseAction(s)
	}
	if s, ok := f.str("updateChannel"); ok {
		u.UpdateChannel = UpdateChannel(s)
	}
	if m, ok := f.object("scenarioModelDefaults"); ok {
		u.ScenarioModelDefaults = map[string]string{}
		for k, v := range m {
			if s, ok := rawString(v); ok {
				u.ScenarioModelDefaults[k] = s
			}
		}
	}
	if persistedVersion < 3 && u.MotionPreference == MotionStandard {
		u.MotionPreference = MotionSystem
	}
	u.Normalize()
	return u
}

// DecodeApp 从 SQLite settings 表的行（键 → JSON 值）解出 Go 侧设置中的标量字段，缺的或类型不对的取默认值。
// 网络代理与 FlareSolverr 由调用方用 Go 侧的归一化函数填入（规则只在那里存一份）。
func DecodeApp(raw map[string]json.RawMessage) App {
	a := DefaultApp()
	f := fields(raw)
	if s, ok := f.str("language"); ok {
		a.Language = Language(s)
	}
	for key, dst := range map[string]*bool{
		"launchAtStartup": &a.LaunchAtStartup, "minimizeOnLaunch": &a.MinimizeOnLaunch,
		"backgroundTasksEnabled": &a.BackgroundTasksEnabled, "enableNotifications": &a.EnableNotifications,
		"backupAutoEnabled": &a.BackupAutoEnabled, "transcribeDiarize": &a.TranscribeDiarize,
	} {
		if b, ok := f.boolean(key); ok {
			*dst = b
		}
	}
	if n, ok := f.number("backupIntervalHours"); ok {
		a.BackupIntervalHours = ClampBackup(n, DefaultBackupIntervalHours, MinBackupIntervalHours, MaxBackupIntervalHours)
	}
	if n, ok := f.number("backupKeepCount"); ok {
		a.BackupKeepCount = ClampBackup(n, DefaultBackupKeepCount, MinBackupKeepCount, MaxBackupKeepCount)
	}
	if s, ok := f.str("ytDlpPath"); ok {
		a.YtDlpPath = s
	}
	if s, ok := f.str("ffmpegPath"); ok {
		a.FfmpegPath = s
	}
	a.Normalize()
	return a
}
