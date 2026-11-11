package main

// 对应 apps/desktop/src/main/ipc/settings.ipc.ts 的 settings:get / settings:set。
//
// 尚未迁移、并且不静默吞掉的部分：
//   - 网络代理生效（applyNetworkProxySettings）：写库后同步给 activeNetProxy（netproxy.go）；Electron 会话代理不适用。
//   - 可执行文件路径白名单（pickedPaths）：由 ytdlp/ffmpeg:pickBinary 登记（managed_tools.go），只接受空串或登记过的路径。
//
// 与 TS 一致的一个特性：settings:set 先写 AI 配置文件，再校验并写库。
// 所以同一批里 AI 部分合法、flareSolverr 非法时，AI 配置已落盘而其余设置未写入（TS 原本如此，未改）。
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

// persistedSettingsKeys 是 settings 表里允许读写的键（双向白名单），
// 表里还有 master_password 等主进程内部状态，不能回传给渲染进程，也不能被它写入。
var persistedSettingsKeys = map[string]bool{
	"theme": true, "language": true, "autoSave": true, "backgroundImageFileName": true,
	"backgroundImageOpacity": true, "backgroundImageBlur": true, "updateChannel": true,
	"launchAtStartup": true, "minimizeOnLaunch": true, "backgroundTasksEnabled": true,
	"enableNotifications": true, "networkProxy": true, "flareSolverr": true,
	"backupAutoEnabled": true, "backupIntervalHours": true, "backupKeepCount": true,
	"ytDlpPath": true, "ffmpegPath": true, "transcribeDiarize": true,
}

// 会被主进程 spawn 的可执行文件路径，只接受文件选择器返回值（或空串）
var executablePathKeys = map[string]bool{"ytDlpPath": true, "ffmpegPath": true}

var aiSettingsKeys = map[string]bool{
	"aiProvider": true, "aiApiProtocol": true, "aiApiKey": true, "aiApiUrl": true, "aiModel": true,
	"aiProviders": true, "aiModels": true, "modelRouteDefaults": true,
}

// defaultSettings 必须与 packages/shared/types/settings.ts 的 DEFAULT_SETTINGS 一致；
// settings_test.go 用 golden 里 TS 导出的值校验，防止两边漂移。
func defaultSettings() map[string]any {
	return map[string]any{
		"theme": "system", "language": "zh", "autoSave": true,
		"backgroundImageOpacity": 0.22, "backgroundImageBlur": 14.0,
		"updateChannel": "stable", "backgroundTasksEnabled": false,
		"networkProxy": map[string]any{
			"mode": "system", "protocol": "http", "host": "", "port": 7890.0,
			"username": "", "password": "", "bypass": "<local>,localhost,127.0.0.1,::1",
		},
		"backupAutoEnabled": true, "backupIntervalHours": 24.0, "backupKeepCount": 10.0,
	}
}

func (l *Library) settingsGet(ctx context.Context) (any, error) {
	settings := defaultSettings()
	rows, err := l.db.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		key, _ := r[0].(string)
		if !persistedSettingsKeys[key] {
			continue
		}
		raw, isString := r[1].(string)
		if !isString {
			settings[key] = nil // JSON.parse(null) === null
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			settings[key] = raw // 解析失败按原始字符串返回
		} else {
			settings[key] = parsed
		}
	}
	l.mergeSharedAIConfig(settings)
	return settings, nil
}

// ── FlareSolverr 设置校验，对应 packages/shared/utils/flaresolverr.ts ──

var sshHostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$`)

type flareSolverrSettings struct {
	Enabled    bool   `json:"enabled"`
	Connection string `json:"connection"`
	Port       int    `json:"port"`
	SSHHost    string `json:"sshHost"`
}

func normalizeFlareSolverr(value any) flareSolverrSettings {
	in, _ := value.(map[string]any)
	out := flareSolverrSettings{Connection: "local", Port: 8191}
	if enabled, _ := in["enabled"].(bool); enabled {
		out.Enabled = true
	}
	if in["connection"] == "ssh" {
		out.Connection = "ssh"
	}
	if p, ok := in["port"].(float64); ok && p == math.Trunc(p) && p > 0 && p <= 65535 {
		out.Port = int(p)
	}
	if h, ok := in["sshHost"].(string); ok && sshHostPattern.MatchString(strings.TrimFunc(h, jsSpace)) {
		out.SSHHost = strings.TrimFunc(h, jsSpace)
	}
	return out
}

func validateFlareSolverr(value any) (flareSolverrSettings, error) {
	invalid := errors.New("FlareSolverr 设置无效，请填写有效端口和 SSH 主机别名")
	result := normalizeFlareSolverr(value)
	in, ok := value.(map[string]any)
	if !ok {
		return result, invalid
	}
	_, enabledOK := in["enabled"].(bool)
	conn, connOK := in["connection"].(string)
	port, portOK := in["port"].(float64)
	host, hostOK := in["sshHost"].(string)
	if !enabledOK || !connOK || (conn != "local" && conn != "ssh") ||
		!portOK || int(port) != result.Port || float64(result.Port) != port ||
		!hostOK || host != result.SSHHost {
		return result, invalid
	}
	if result.Enabled && result.Connection == "ssh" && result.SSHHost == "" {
		return result, errors.New("请填写已有 SSH 配置中的主机别名")
	}
	return result, nil
}

// ── settings:set ──

type rawEntry struct {
	key   string
	value json.RawMessage
}

// orderedEntries 按出现顺序取出 JSON 对象的键值，对应 Object.entries。
func orderedEntries(raw json.RawMessage) ([]rawEntry, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var out []rawEntry
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		key := keyTok.(string)
		replaced := false
		for i := range out { // 重复键：JS 保留首次位置、取最后的值
			if out[i].key == key {
				out[i].value, replaced = v, true
			}
		}
		if !replaced {
			out = append(out, rawEntry{key, v})
		}
	}
	return out, nil
}

func isAcceptableBinaryPath(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	return strings.TrimFunc(s, jsSpace) == "" || isPickedBinaryPath(s) // 空串表示清除；非空只接受文件选择器返回过的路径
}

func (l *Library) settingsSet(ctx context.Context, raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, errors.New("Cannot convert undefined or null to object")
	}
	if trimmed[0] != '{' {
		return true, nil // 数组、字符串等：Object.keys 给出的都不在白名单里，没有可写入项
	}
	entries, err := orderedEntries(trimmed)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if aiSettingsKeys[e.key] {
			payload, err := parseOrdered(trimmed)
			if err != nil {
				return nil, err
			}
			if err := l.persistSharedAIConfig(payload.(*omap)); err != nil {
				return nil, err
			}
			break
		}
	}

	type write struct{ key, value string }
	var writes []write
	var rejected []string
	for _, e := range entries {
		if aiSettingsKeys[e.key] {
			continue // 已在上面写入 ai-models.json，不进 settings 表
		}
		if !persistedSettingsKeys[e.key] || (executablePathKeys[e.key] && !isAcceptableBinaryPath(e.value)) {
			rejected = append(rejected, e.key)
			continue
		}
		value := e.value
		if e.key == "flareSolverr" {
			var parsed any
			_ = json.Unmarshal(e.value, &parsed)
			v, err := validateFlareSolverr(parsed)
			if err != nil {
				return nil, err // 先校验后写入：任一键非法则整批不写
			}
			value, _ = json.Marshal(v)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, value); err != nil {
			return nil, err
		}
		writes = append(writes, write{e.key, compact.String()})
	}
	if len(rejected) > 0 {
		log.Printf("[settings] 拒绝写入非白名单键: %s", strings.Join(rejected, ", "))
	}
	if len(writes) == 0 {
		return true, nil
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	stmts := make([]sqlite.Statement, len(writes))
	for i, w := range writes {
		stmts[i] = sqlite.Statement{SQL: "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", Args: []any{w.key, w.value}}
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, fmt.Errorf("保存设置失败: %w", err)
	}
	for _, w := range writes {
		if w.key == "networkProxy" {
			var v any
			_ = json.Unmarshal([]byte(w.value), &v)
			activeNetProxy.Apply(v) // 对应 applyNetworkProxySettings
		}
	}
	return true, nil
}

func (l *Library) registerSettings(b *Bridge) {
	b.handlers["settings:get"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return l.settingsGet(ctx)
	}
	b.handlers["settings:set"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.settingsSet(ctx, argAt(a, 0))
	}
}
