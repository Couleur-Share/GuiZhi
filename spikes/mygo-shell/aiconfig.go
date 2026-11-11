package main

// 对应 packages/core/src/ai-config.ts 中 settings 用到的部分：读取、归一化、整体替换 config/ai-models.json，
// 以及 settings.ipc.ts 里的 mergeSharedAIConfig / mergeAISettingsPayload / buildLegacyAIModel。
//
// 文件里的 apiKey 与 TS 一致，以明文保存在数据目录的 config 下；本文件只迁移行为，不改变存储方式。
// 配置经 settings 之外的入口（CLI、MCP）增删单个供应商/模型的方法（addProvider 等）不在这里。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const aiConfigFileName = "ai-models.json"

var modelRoutes = []string{"mainText", "fastText", "visionText", "embedding", "audioText", "imageGen"}

// ── 保持键序的 JSON 对象：输出文件的字段顺序与 TS 一致，读起来也稳定 ──

type omap struct {
	keys []string
	vals map[string]any
}

func newOmap() *omap { return &omap{vals: map[string]any{}} }

func (m *omap) set(k string, v any) *omap {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
	return m
}

func (m *omap) get(k string) (any, bool) { v, ok := m.vals[k]; return v, ok }

func (m *omap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := marshalNoEscape(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// parseOrdered 把 JSON 解析成 *omap / []any / 标量，对象保留键序（重复键取最后的值、首次位置）。
func parseOrdered(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	v, err := decodeOrdered(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("JSON 末尾有多余内容")
	}
	return v, nil
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			m := newOmap()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				m.set(kt.(string), v)
			}
			_, err := dec.Token()
			return m, err
		}
		arr := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		_, err := dec.Token()
		return arr, err
	default:
		return tok, nil // string / float64 / bool / nil
	}
}

// ── 与 JS 语义对齐的小工具 ──

type aiConfigError struct{ msg string }

func (e *aiConfigError) Error() string { return e.msg }

// trimOpt 对应 `value?.trim()`：null/undefined 得到 nullish，字符串去空白，其余类型像 JS 一样抛 TypeError。
func trimOpt(v any, present bool) (s string, nullish bool, err error) {
	if !present || v == nil {
		return "", true, nil
	}
	str, ok := v.(string)
	if !ok {
		return "", false, errors.New("value?.trim is not a function")
	}
	return strings.TrimFunc(str, jsSpace), false, nil
}

func assertNonEmpty(m *omap, key, name string) (string, error) {
	v, ok := m.get(key)
	s, _, err := trimOpt(v, ok)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", &aiConfigError{name + " 不能为空"}
	}
	return s, nil
}

// optTrim 对应 `x?.trim() || undefined`：空串与缺失都不输出该字段。
func optTrim(m *omap, key string) (string, bool, error) {
	v, ok := m.get(key)
	s, _, err := trimOpt(v, ok)
	return s, s != "", err
}

func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// asObject 对应 JS 的属性访问：null 抛 TypeError；字符串、数字、数组等取任何属性都是 undefined，
// 于是后面的必填项检查会给出「xxx 不能为空」。
func asObject(v any, what string) (*omap, error) {
	if m, ok := v.(*omap); ok {
		return m, nil
	}
	if v == nil {
		return nil, fmt.Errorf("Cannot read properties of null（%s）", what)
	}
	return newOmap(), nil
}

func normalizeProtocol(raw any, provider, apiURL string) string {
	if s, ok := raw.(string); ok && (s == "openai" || s == "gemini" || s == "anthropic") {
		return s
	}
	p, u := strings.ToLower(provider), strings.ToLower(apiURL)
	switch {
	case strings.Contains(p, "anthropic") || strings.Contains(p, "claude") || strings.Contains(u, "api.anthropic.com"):
		return "anthropic"
	case strings.Contains(p, "gemini") || strings.Contains(p, "google") || strings.Contains(u, "generativelanguage.googleapis.com"):
		return "gemini"
	}
	return "openai"
}

func normalizeCapabilities(v any) *omap {
	out := newOmap()
	chat := true
	if m, ok := v.(*omap); ok {
		for _, k := range m.keys {
			out.set(k, m.vals[k])
		}
		if c, ok := m.get("chat"); ok && c == false {
			chat = false
		}
	}
	return out.set("chat", chat)
}

func apiKeyOf(m *omap) string {
	if v, ok := m.get("apiKey"); ok {
		if s, isStr := v.(string); isStr {
			return strings.TrimFunc(s, jsSpace)
		}
	}
	return ""
}

func enabledOf(m *omap) bool {
	v, ok := m.get("enabled")
	return !ok || v != false
}

func passthrough(m *omap, key string, out *omap) {
	if v, ok := m.get(key); ok {
		out.set(key, v)
	}
}

func normalizeProvider(v any) (*omap, error) {
	p, err := asObject(v, "供应商配置")
	if err != nil {
		return nil, err
	}
	providerName, err := assertNonEmpty(p, "provider", "provider")
	if err != nil {
		return nil, err
	}
	apiURL, err := assertNonEmpty(p, "apiUrl", "apiUrl")
	if err != nil {
		return nil, err
	}
	id, err := assertNonEmpty(p, "id", "provider id")
	if err != nil {
		return nil, err
	}
	name, hasName, err := optTrim(p, "name")
	if err != nil {
		return nil, err
	}
	proto, _ := p.get("apiProtocol")
	out := newOmap().set("id", id)
	if hasName {
		out.set("name", name)
	}
	out.set("provider", providerName).set("apiProtocol", normalizeProtocol(proto, providerName, apiURL)).
		set("apiKey", apiKeyOf(p)).set("apiUrl", apiURL)
	passthrough(p, "lastVerifiedAt", out)
	return out.set("enabled", enabledOf(p)), nil
}

func normalizeModel(v any) (*omap, error) {
	m, err := asObject(v, "模型配置")
	if err != nil {
		return nil, err
	}
	modelName, err := assertNonEmpty(m, "model", "model")
	if err != nil {
		return nil, err
	}
	provider, err := assertNonEmpty(m, "provider", "model provider")
	if err != nil {
		return nil, err
	}
	apiURL, err := assertNonEmpty(m, "apiUrl", "model apiUrl")
	if err != nil {
		return nil, err
	}
	id, err := assertNonEmpty(m, "id", "model id")
	if err != nil {
		return nil, err
	}
	name, hasName, err := optTrim(m, "name")
	if err != nil {
		return nil, err
	}
	providerID, hasProviderID, err := optTrim(m, "providerId")
	if err != nil {
		return nil, err
	}
	proto, _ := m.get("apiProtocol")
	caps, _ := m.get("capabilities")
	isDefault, _ := m.get("isDefault")
	out := newOmap().set("id", id)
	if hasName {
		out.set("name", name)
	}
	if hasProviderID {
		out.set("providerId", providerID)
	}
	out.set("provider", provider).set("apiProtocol", normalizeProtocol(proto, provider, apiURL)).
		set("apiKey", apiKeyOf(m)).set("apiUrl", apiURL).set("model", modelName).
		set("isDefault", isDefault == true)
	passthrough(m, "lastVerifiedAt", out)
	return out.set("capabilities", normalizeCapabilities(caps)).set("enabled", enabledOf(m)), nil
}

func normalizeRoutes(v any) *omap {
	out := newOmap()
	m, ok := v.(*omap)
	if !ok {
		return out
	}
	for _, route := range modelRoutes {
		if id, ok := m.get(route); ok {
			if s, isStr := id.(string); isStr && strings.TrimFunc(s, jsSpace) != "" {
				out.set(route, strings.TrimFunc(s, jsSpace))
			}
		}
	}
	return out
}

type aiConfigFile struct {
	UpdatedAt string
	Providers []*omap
	Models    []*omap
	Routes    *omap
}

func nowIso() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func parseAIConfig(raw []byte) (*aiConfigFile, error) {
	v, err := parseOrdered(raw)
	if err != nil {
		return nil, err
	}
	root, ok := v.(*omap)
	if v == nil {
		return nil, errors.New("Cannot read properties of null (reading 'kind')")
	}
	if !ok {
		return nil, &aiConfigError{"AI 配置文件格式不受支持"}
	}
	if kind, _ := root.get("kind"); kind != "guizhi-ai-config" {
		return nil, &aiConfigError{"AI 配置文件格式不受支持"}
	}
	if version, _ := root.get("version"); version != float64(1) {
		return nil, &aiConfigError{"AI 配置文件格式不受支持"}
	}
	cfg := &aiConfigFile{UpdatedAt: nowIso(), Routes: newOmap()}
	if s, ok := root.vals["updatedAt"].(string); ok {
		cfg.UpdatedAt = s
	}
	if arr, ok := root.vals["providers"].([]any); ok {
		for _, p := range arr {
			n, err := normalizeProvider(p)
			if err != nil {
				return nil, err
			}
			cfg.Providers = append(cfg.Providers, n)
		}
	}
	if arr, ok := root.vals["models"].([]any); ok {
		for _, m := range arr {
			n, err := normalizeModel(m)
			if err != nil {
				return nil, err
			}
			cfg.Models = append(cfg.Models, n)
		}
	}
	cfg.Routes = normalizeRoutes(root.vals["modelRouteDefaults"])
	return cfg, nil
}

func (l *Library) aiConfigPath() string { return filepath.Join(l.configDir, aiConfigFileName) }

var errNoConfigDir = errors.New("未指定 config 目录（-config），无法读写 AI 配置")

// readAIConfig 对应 CoreAIConfigService.read：文件不存在得到空配置，坏文件报错。
func (l *Library) readAIConfig() (*aiConfigFile, error) {
	if l.configDir == "" {
		return nil, errNoConfigDir
	}
	raw, err := os.ReadFile(l.aiConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return &aiConfigFile{UpdatedAt: nowIso(), Routes: newOmap()}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseAIConfig(raw)
}

// replaceAIConfig 对应 CoreAIConfigService.replace → write：逐项归一化，任一项非法则整体不写。
// 与 TS 的 writeFileSync 不同，这里先写临时文件再改名，崩溃时不会留下半截配置。
func (l *Library) replaceAIConfig(providers, models []any, routes any) error {
	if l.configDir == "" {
		return errNoConfigDir
	}
	cfg := &aiConfigFile{UpdatedAt: nowIso()}
	for _, p := range providers {
		n, err := normalizeProvider(p)
		if err != nil {
			return err
		}
		cfg.Providers = append(cfg.Providers, n)
	}
	for _, m := range models {
		n, err := normalizeModel(m)
		if err != nil {
			return err
		}
		cfg.Models = append(cfg.Models, n)
	}
	cfg.Routes = normalizeRoutes(routes)

	out := newOmap().set("kind", "guizhi-ai-config").set("version", 1).set("updatedAt", cfg.UpdatedAt).
		set("providers", omapList(cfg.Providers)).set("models", omapList(cfg.Models)).set("modelRouteDefaults", cfg.Routes)
	var buf bytes.Buffer
	raw, err := out.MarshalJSON()
	if err != nil {
		return err
	}
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return err
	}
	text := strings.ReplaceAll(strings.ReplaceAll(buf.String(), `\u2028`, "\u2028"), `\u2029`, "\u2029") + "\n"
	if err := os.MkdirAll(l.configDir, 0o755); err != nil {
		return err
	}
	tmp := l.aiConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.aiConfigPath())
}

func omapList(ms []*omap) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

// ── settings.ipc.ts 的合并逻辑 ──

// mergeSharedAIConfig 把 AI 配置并入 settings 返回值；任何失败只放弃合并（TS 里是 console.warn）。
func (l *Library) mergeSharedAIConfig(settings map[string]any) {
	cfg, err := l.readAIConfig()
	if err != nil {
		return
	}
	if len(cfg.Providers) > 0 {
		settings["aiProviders"] = omapList(cfg.Providers)
	}
	if len(cfg.Models) > 0 {
		settings["aiModels"] = omapList(cfg.Models)
		isChat := func(m *omap) bool {
			caps, _ := m.vals["capabilities"].(*omap)
			return caps == nil || caps.vals["chat"] != false
		}
		var chosen *omap
		for _, m := range cfg.Models {
			if isChat(m) && m.vals["isDefault"] == true {
				chosen = m
				break
			}
		}
		if chosen == nil {
			for _, m := range cfg.Models {
				if isChat(m) {
					chosen = m
					break
				}
			}
		}
		if chosen != nil {
			settings["aiProvider"] = chosen.vals["provider"]
			settings["aiApiProtocol"] = chosen.vals["apiProtocol"]
			settings["aiApiKey"] = chosen.vals["apiKey"]
			settings["aiApiUrl"] = chosen.vals["apiUrl"]
			settings["aiModel"] = chosen.vals["model"]
		}
	}
	if len(cfg.Routes.keys) > 0 {
		settings["modelRouteDefaults"] = cfg.Routes
	}
}

func buildLegacyAIModel(payload *omap) (*omap, error) {
	trimmed := func(key string) (string, bool, error) {
		v, ok := payload.get(key)
		s, _, err := trimOpt(v, ok)
		return s, s != "", err
	}
	provider, ok1, err := trimmed("aiProvider")
	if err != nil {
		return nil, err
	}
	proto, _ := payload.get("aiApiProtocol")
	if !ok1 || !jsTruthy(proto) {
		return nil, nil
	}
	apiKey, ok2, err := trimmed("aiApiKey")
	if err != nil {
		return nil, err
	}
	if !ok2 {
		return nil, nil
	}
	apiURL, ok3, err := trimmed("aiApiUrl")
	if err != nil {
		return nil, err
	}
	if !ok3 {
		return nil, nil
	}
	model, ok4, err := trimmed("aiModel")
	if err != nil {
		return nil, err
	}
	if !ok4 {
		return nil, nil
	}
	return newOmap().set("id", "model_legacy_default").set("provider", provider).set("apiProtocol", proto).
		set("apiKey", apiKey).set("apiUrl", apiURL).set("model", model).set("isDefault", true).
		set("capabilities", newOmap().set("chat", true)), nil
}

// persistSharedAIConfig 对应 settings:set 里的同名函数：载荷里有 AI 键才读当前配置、合并、整体替换。
func (l *Library) persistSharedAIConfig(payload *omap) error {
	current, err := l.readAIConfig()
	if err != nil {
		return err
	}
	providers, models := omapList(current.Providers), omapList(current.Models)
	var routes any = current.Routes
	if arr, ok := payload.vals["aiProviders"].([]any); ok {
		providers = arr
	}
	if arr, ok := payload.vals["aiModels"].([]any); ok {
		models = arr
	}
	switch r := payload.vals["modelRouteDefaults"].(type) {
	case *omap, []any:
		routes = r // typeof === "object"：数组也算，归一化时会得到空对象
	}
	legacy, err := buildLegacyAIModel(payload)
	if err != nil {
		return err
	}
	if len(models) == 0 && legacy != nil {
		models = []any{legacy}
	}
	return l.replaceAIConfig(providers, models, routes)
}
