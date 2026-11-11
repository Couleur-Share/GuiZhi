package main

// 配置迁移：config:export / config:read / config:apply。
// 对应 config-transfer.ipc.ts、services/config-transfer（index / ai-reconcile / config-crypto）与
// packages/shared/utils/config-transfer.ts。
//
// 读与应用刻意拆成两步：导入会覆盖本机全部设置，用户得先看清文件里有什么再决定。
// 加密格式与 TS 版互通：`ENC::` + base64(iv 12B | tag 16B | 密文)，密钥由 scrypt 派生，参数随文件走。

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	chConfigExport = "config:export"
	chConfigRead   = "config:read"
	chConfigApply  = "config:apply"

	configTransferKind    = "guizhi-config"
	configTransferVersion = 1
	configTransferCanary  = "guizhi-config-transfer"

	funasrProviderID = "provider_local_funasr"
	funasrModelID    = "model_local_sensevoice"
	funasrPort       = "8620"

	snapshotDirPrefix = "pre-import-"
	snapshotKeepCount = 3
)

// 与本机绑定、导过去只会坏事的设置字段（NON_PORTABLE_SETTINGS_KEYS）。
var nonPortableSettingsKeys = map[string]bool{
	"dataPath": true, "ytDlpPath": true, "ffmpegPath": true, "launchAtStartup": true,
	"backgroundImageFileName": true, "settingsUpdatedAt": true, "isDarkMode": true,
}

// 随配置走的界面偏好 localStorage 白名单（TRANSFER_LOCAL_STORAGE_KEYS）。
var transferLocalStorageKeys = map[string]bool{
	"ui-storage": true, "guizhi-library-table-config": true, "guizhi-table-config": true,
}

// 快照要拷的配置文件；不存在的跳过。
var configSnapshotFiles = []string{"ai-models.json", "illustration-styles.json", "shortcuts.json", "shortcut-mode.json", mcpScopeFileName}

type configState struct {
	mu           sync.Mutex
	lastReadPath string // 上一次经 config:read 选中的文件；apply 只接受这个路径
	hasRead      bool
}

// ── shared/utils/config-transfer.ts ──

func isPlainRecord(v any) (*omap, bool) {
	m, ok := v.(*omap)
	return m, ok && m != nil
}

func asRecordArray(v any) []*omap {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []*omap
	for _, e := range arr {
		if m, ok := isPlainRecord(e); ok {
			out = append(out, m)
		}
	}
	return out
}

// jsEntries 对应 Object.entries(value)：对象取自有键，数组取下标，字符串取各个码元，其余为空。
func jsEntries(v any) (keys []string, vals []any) {
	switch x := v.(type) {
	case *omap:
		if x == nil {
			return
		}
		for _, k := range x.keys {
			keys = append(keys, k)
			vals = append(vals, x.vals[k])
		}
	case []any:
		for i, e := range x {
			keys = append(keys, fmt.Sprint(i))
			vals = append(vals, e)
		}
	case string:
		for i, u := range toUTF16(x) {
			keys = append(keys, fmt.Sprint(i))
			vals = append(vals, fromU16([]uint16{u}))
		}
	}
	return
}

// stripNonPortableSettings 剔除机器绑定字段，返回新对象。
func stripNonPortableSettings(values any) *omap {
	out := newOmap()
	keys, vals := jsEntries(values)
	for i, k := range keys {
		if nonPortableSettingsKeys[k] {
			continue
		}
		out.set(k, vals[i])
	}
	return out
}

// pickTransferableLocalStorage 按白名单收敛界面偏好，导出与导入两个方向都过一遍。
func pickTransferableLocalStorage(entries any) *omap {
	out := newOmap()
	m, ok := isPlainRecord(entries)
	if !ok {
		return out
	}
	for _, k := range m.keys {
		if transferLocalStorageKeys[k] {
			out.set(k, m.vals[k])
		}
	}
	return out
}

type secretTransform func(value string) (string, bool)

type secretStats struct{ processed, failed int }

// mapConfigSecrets 就地替换设置里的全部机密字段：导出加密与导入解密共用这一个遍历器。
func mapConfigSecrets(settings *omap, transform secretTransform) secretStats {
	var stats secretStats
	visit := func(holder *omap, key string) {
		if holder == nil {
			return
		}
		cur, ok := holder.vals[key].(string)
		if !ok || cur == "" {
			return
		}
		next, ok := transform(cur)
		if !ok {
			stats.failed++
			return
		}
		holder.vals[key] = next
		stats.processed++
	}
	if settings == nil {
		return stats
	}
	visit(settings, "aiApiKey")
	for _, p := range asRecordArray(settings.vals["aiProviders"]) {
		visit(p, "apiKey")
	}
	for _, m := range asRecordArray(settings.vals["aiModels"]) {
		visit(m, "apiKey")
	}
	if np, ok := isPlainRecord(settings.vals["networkProxy"]); ok {
		visit(np, "password")
	}
	return stats
}

type configEncryption struct {
	Salt    string
	N, R, P int
	Canary  string
}

type configShortcutsPart struct {
	Accelerators *omap
	Modes        *omap
}

type configTransferFile struct {
	Version            float64
	ExportedAt         string
	AppVersion         string
	Encryption         *configEncryption
	Settings           *omap
	SettingsVersion    any // 数字时才有意义
	HasSettingsVersion bool
	UILayout           *omap
	IllustrationStyles []any
	HasStyles          bool
	Shortcuts          *configShortcutsPart
	McpScope           *mcpScope
}

func jsIsInteger(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	return f, true
}

// parseConfigEncryption 校验加密块：scrypt 的代价参数来自文件，要卡在合理区间，
// 构造过的文件不能靠一个巨大的 N 把主进程拖死。
func parseConfigEncryption(raw any, present bool) (*configEncryption, string) {
	if !present || raw == nil {
		return nil, ""
	}
	m, ok := isPlainRecord(raw)
	if !ok {
		return nil, "加密信息格式不正确"
	}
	if m.vals["algo"] != "aes-256-gcm" || m.vals["kdf"] != "scrypt" {
		return nil, "不支持的加密方式"
	}
	salt, ok1 := m.vals["salt"].(string)
	canary, ok2 := m.vals["canary"].(string)
	if !ok1 || salt == "" || !ok2 || canary == "" {
		return nil, "加密信息不完整"
	}
	n, okN := jsIsInteger(m.vals["n"])
	if !okN || n < 4096 || n > 65536 || int64(n)&(int64(n)-1) != 0 {
		return nil, "加密参数超出支持范围"
	}
	r, okR := jsIsInteger(m.vals["r"])
	p, okP := jsIsInteger(m.vals["p"])
	if !okR || r < 1 || r > 8 || !okP || p < 1 || p > 4 {
		return nil, "加密参数超出支持范围"
	}
	return &configEncryption{Salt: salt, N: int(n), R: int(r), P: int(p), Canary: canary}, ""
}

// parseConfigTransferFile 校验信封并收敛可选字段；更高版本一律拒绝。
func parseConfigTransferFile(raw any) (*configTransferFile, string) {
	root, ok := isPlainRecord(raw)
	if !ok || root.vals["kind"] != configTransferKind {
		return nil, "这不是一份归知配置文件"
	}
	version, isInt := jsIsInteger(root.vals["version"])
	if !isInt || version < 1 {
		return nil, "配置文件缺少有效的版本号"
	}
	if version > configTransferVersion {
		return nil, fmt.Sprintf("配置文件来自更新版本的归知（格式 v%s，当前支持 v%d），请先升级应用", jsNumberString(version), configTransferVersion)
	}
	settings, ok := isPlainRecord(root.vals["settings"])
	if !ok {
		return nil, "配置文件里没有可用的设置内容"
	}
	encRaw, encPresent := root.vals["encryption"]
	enc, msg := parseConfigEncryption(encRaw, encPresent)
	if msg != "" {
		return nil, msg
	}
	f := &configTransferFile{Version: version, Encryption: enc, Settings: settings}
	f.ExportedAt, _ = root.vals["exportedAt"].(string)
	f.AppVersion, _ = root.vals["appVersion"].(string)
	if v, ok := root.vals["settingsVersion"].(float64); ok {
		f.SettingsVersion, f.HasSettingsVersion = v, true
	}
	f.UILayout = pickTransferableLocalStorage(root.vals["uiLayout"])
	if arr, ok := root.vals["illustrationStyles"].([]any); ok {
		f.IllustrationStyles, f.HasStyles = arr, true
	}
	if sc, ok := isPlainRecord(root.vals["shortcuts"]); ok {
		part := &configShortcutsPart{Accelerators: newOmap(), Modes: newOmap()}
		if a, ok := isPlainRecord(sc.vals["accelerators"]); ok {
			part.Accelerators = a
		}
		if m, ok := isPlainRecord(sc.vals["modes"]); ok {
			part.Modes = m
		}
		f.Shortcuts = part
	}
	// parseMcpScope 对坏数据回「全部可见」，所以先判在不在，再谈内容对不对
	if ms, ok := isPlainRecord(root.vals["mcpScope"]); ok {
		text, err := ms.MarshalJSON()
		if err == nil {
			if v, err := jsParse(string(text)); err == nil {
				scope := parseMcpScope(v)
				f.McpScope = &scope
			}
		}
	}
	return f, ""
}

func buildConfigPreview(f *configTransferFile) *omap {
	settings := f.Settings
	routeCount := 0
	if routes, ok := isPlainRecord(settings.vals["modelRouteDefaults"]); ok {
		for _, k := range routes.keys {
			if s, ok := routes.vals[k].(string); ok && jsTrim(s) != "" {
				routeCount++
			}
		}
	}
	shortcutCount := 0
	if f.Shortcuts != nil {
		for _, k := range f.Shortcuts.Accelerators.keys {
			if jsTruthy(f.Shortcuts.Accelerators.vals[k]) {
				shortcutCount++
			}
		}
	}
	styleCount := 0
	if f.HasStyles {
		styleCount = len(f.IllustrationStyles)
	}
	p := newOmap().
		set("exportedAt", f.ExportedAt).
		set("appVersion", f.AppVersion).
		set("encrypted", f.Encryption != nil).
		set("providerCount", float64(len(asRecordArray(settings.vals["aiProviders"])))).
		set("modelCount", float64(len(asRecordArray(settings.vals["aiModels"])))).
		set("routeCount", float64(routeCount)).
		set("styleCount", float64(styleCount)).
		set("shortcutCount", float64(shortcutCount)).
		set("uiLayoutKeyCount", float64(len(f.UILayout.keys)))
	if f.McpScope != nil {
		p.set("mcpScope", newOmap().
			set("mode", f.McpScope.Mode).
			set("collectionCount", float64(len(f.McpScope.AllowedCollections))).
			set("allowUncategorized", f.McpScope.AllowUncategorized))
	}
	return p
}

// ── 机密加解密（config-crypto.ts）──

func deriveConfigKey(password string, salt []byte, n, r, p int) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, n, r, p, 32)
}

func encryptConfigText(plain string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(plain), nil) // 密文 || tag
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	buf := append(append(append([]byte{}, iv...), tag...), ct...)
	return "ENC::" + base64.StdEncoding.EncodeToString(buf), nil
}

// nodeBase64Decode 对应 Buffer.from(s, "base64")：宽松——忽略非法字符，兼容 URL 安全字母表，遇 '=' 停止。
func nodeBase64Decode(s string) []byte {
	var clean []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '=':
			i = len(s)
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/':
			clean = append(clean, c)
		case c == '-':
			clean = append(clean, '+')
		case c == '_':
			clean = append(clean, '/')
		}
	}
	if len(clean)%4 == 1 {
		clean = clean[:len(clean)-1]
	}
	out, _ := base64.RawStdEncoding.DecodeString(string(clean))
	return out
}

// decryptConfigText 对应 decryptText：不是 `ENC::` 开头的原样返回；解不开返回 ok=false。
func decryptConfigText(data string, key []byte) (string, bool) {
	if data == "" || !strings.HasPrefix(data, "ENC::") {
		return data, true
	}
	buf := nodeBase64Decode(data[5:])
	if len(buf) < 28 {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	plain, err := gcm.Open(nil, buf[:12], append(append([]byte{}, buf[28:]...), buf[12:28]...), nil)
	if err != nil {
		return "", false
	}
	return string(plain), true
}

func encryptConfigSecrets(settings *omap, password string) (*omap, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	const n, r, p = 16384, 8, 1
	key, err := deriveConfigKey(password, salt, n, r, p)
	if err != nil {
		return nil, err
	}
	canary, err := encryptConfigText(configTransferCanary, key)
	if err != nil {
		return nil, err
	}
	var encErr error
	mapConfigSecrets(settings, func(v string) (string, bool) {
		out, err := encryptConfigText(v, key)
		if err != nil {
			encErr = err
			return "", false
		}
		return out, true
	})
	if encErr != nil {
		return nil, encErr
	}
	return newOmap().
		set("algo", "aes-256-gcm").set("kdf", "scrypt").
		set("salt", base64.StdEncoding.EncodeToString(salt)).
		set("n", float64(n)).set("r", float64(r)).set("p", float64(p)).
		set("canary", canary), nil
}

type configDecryptResult struct {
	ok            bool
	wrongPassword bool
	failed        int
	err           string
}

func decryptConfigSecrets(f *configTransferFile, password string) configDecryptResult {
	enc := f.Encryption
	if enc == nil {
		return configDecryptResult{ok: true}
	}
	if password == "" {
		return configDecryptResult{wrongPassword: true, err: "这份配置需要密码才能打开"}
	}
	key, err := deriveConfigKey(password, nodeBase64Decode(enc.Salt), enc.N, enc.R, enc.P)
	if err != nil {
		return configDecryptResult{err: "无法按文件里的参数派生密钥：" + err.Error()}
	}
	if got, ok := decryptConfigText(enc.Canary, key); !ok || got != configTransferCanary {
		return configDecryptResult{wrongPassword: true, err: "密码不正确"}
	}
	stats := mapConfigSecrets(f.Settings, func(v string) (string, bool) { return decryptConfigText(v, key) })
	f.Encryption = nil
	return configDecryptResult{ok: true, failed: stats.failed}
}

// ── AI 配置对账（ai-reconcile.ts）──

func nonEmptyText(v any) bool {
	s, ok := v.(string)
	return ok && jsTrim(s) != ""
}

func isUsableProvider(v any) (*omap, bool) {
	m, ok := isPlainRecord(v)
	if !ok || !nonEmptyText(m.vals["id"]) || !nonEmptyText(m.vals["provider"]) || !nonEmptyText(m.vals["apiUrl"]) {
		return nil, false
	}
	return m, true
}

func isUsableModel(v any) (*omap, bool) {
	m, ok := isPlainRecord(v)
	if !ok || !nonEmptyText(m.vals["id"]) || !nonEmptyText(m.vals["provider"]) || !nonEmptyText(m.vals["apiUrl"]) || !nonEmptyText(m.vals["model"]) {
		return nil, false
	}
	return m, true
}

// isManagedFunasrURL 对应 isManagedFunasrUrl：指向托管的本地转写服务（127.0.0.1 / localhost 的 8620 端口）。
func isManagedFunasrURL(apiURL any) bool {
	s, ok := apiURL.(string)
	if !ok {
		return false
	}
	s = strings.TrimSuffix(jsTrim(s), "#")
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "127.0.0.1" || host == "localhost") && u.Port() == funasrPort
}

func isLocalEngineProviderRec(p *omap) bool {
	return p.vals["id"] == funasrProviderID || isManagedFunasrURL(p.vals["apiUrl"])
}

func isLocalEngineModelRec(m *omap) bool {
	return m.vals["id"] == funasrModelID || m.vals["providerId"] == funasrProviderID || isManagedFunasrURL(m.vals["apiUrl"])
}

type aiReconcileResult struct {
	providers []any
	models    []any
	routes    *omap
	warnings  []any
}

func reconcileImportedAIConfig(importedProviders, importedModels, importedRoutes any, local *aiConfigFile) aiReconcileResult {
	var warnings []any
	rawProviders, _ := importedProviders.([]any)
	rawModels, _ := importedModels.([]any)

	var usableProviders, usableModels []*omap
	for _, p := range rawProviders {
		if m, ok := isUsableProvider(p); ok {
			usableProviders = append(usableProviders, m)
		}
	}
	for _, m := range rawModels {
		if r, ok := isUsableModel(m); ok {
			usableModels = append(usableModels, r)
		}
	}
	if dropped := len(rawProviders) - len(usableProviders) + len(rawModels) - len(usableModels); dropped > 0 {
		warnings = append(warnings, fmt.Sprintf("已跳过 %d 条字段不完整的服务商 / 模型记录", dropped))
	}

	var providers, models []*omap
	for _, p := range usableProviders {
		if !isLocalEngineProviderRec(p) {
			providers = append(providers, p)
		}
	}
	for _, m := range usableModels {
		if !isLocalEngineModelRec(m) {
			models = append(models, m)
		}
	}
	if len(providers) != len(usableProviders) || len(models) != len(usableModels) {
		warnings = append(warnings, "配置里的本地转写引擎条目已跳过：它指向导出设备上的本地服务，换台机器点了必然失败")
	}

	// 本机自己的内置引擎条目原样保留
	var localProviders, localModels []*omap
	for _, p := range local.Providers {
		if u, ok := isUsableProvider(p); ok && isLocalEngineProviderRec(u) {
			localProviders = append(localProviders, u)
		}
	}
	for _, m := range local.Models {
		if u, ok := isUsableModel(m); ok && isLocalEngineModelRec(u) {
			localModels = append(localModels, u)
		}
	}
	if len(localModels) > 0 {
		warnings = append(warnings, "已保留本机安装的本地转写引擎条目")
	}
	providerIDs := map[string]bool{}
	for _, p := range providers {
		providerIDs[p.vals["id"].(string)] = true
	}
	for _, p := range localProviders {
		if id := p.vals["id"].(string); !providerIDs[id] {
			providers = append(providers, p)
			providerIDs[id] = true
		}
	}
	modelIDs := map[string]bool{}
	for _, m := range models {
		modelIDs[m.vals["id"].(string)] = true
	}
	for _, m := range localModels {
		if id := m.vals["id"].(string); !modelIDs[id] {
			models = append(models, m)
			modelIDs[id] = true
		}
	}

	// 指向不存在模型的路由要清掉：留着就是一条界面上看着配好了、用起来找不到模型的死路由
	routes := newOmap()
	dangling := 0
	if src, ok := isPlainRecord(importedRoutes); ok {
		for _, k := range src.keys {
			s, ok := src.vals[k].(string)
			if !ok || jsTrim(s) == "" {
				continue
			}
			id := jsTrim(s)
			if modelIDs[id] {
				routes.set(k, id)
			} else {
				dangling++
			}
		}
	}
	if dangling > 0 {
		warnings = append(warnings, fmt.Sprintf("已清空 %d 条指向不存在模型的路由", dangling))
	}
	// 导入方没指定语音转写路由时，把本机原来指向内置引擎的那条接回去
	if v, _ := routes.get("audioText"); !jsTruthy(v) {
		if localAudio, _ := local.Routes.get("audioText"); jsTruthy(localAudio) {
			for _, m := range localModels {
				if m.vals["id"] == localAudio {
					routes.set("audioText", localAudio)
					break
				}
			}
		}
	}
	out := aiReconcileResult{routes: routes, warnings: warnings}
	for _, p := range providers {
		out.providers = append(out.providers, p)
	}
	for _, m := range models {
		out.models = append(out.models, m)
	}
	return out
}

// ── 快照（services/config-transfer/index.ts）──

func snapshotStamp(t time.Time) string { return t.Format("20060102-150405") }

func (l *Library) pruneConfigSnapshots() {
	entries, err := os.ReadDir(l.configDir)
	if err != nil {
		logAppErrorf("config-transfer", "清理旧快照", "%s", err.Error())
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), snapshotDirPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i, n := range names {
		if i >= snapshotKeepCount {
			_ = os.RemoveAll(filepath.Join(l.configDir, n))
		}
	}
}

// snapshotConfigDir 应用导入前把 config 目录下的配置文件拷一份。没有任何可拷的文件时返回空串。
func (l *Library) snapshotConfigDir() (string, error) {
	var present []string
	for _, n := range configSnapshotFiles {
		if _, err := os.Stat(filepath.Join(l.configDir, n)); err == nil {
			present = append(present, n)
		}
	}
	if len(present) == 0 {
		return "", nil
	}
	target := filepath.Join(l.configDir, snapshotDirPrefix+snapshotStamp(time.Now()))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	for _, n := range present {
		data, err := os.ReadFile(filepath.Join(l.configDir, n))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(target, n), data, 0o644); err != nil {
			return "", err
		}
	}
	l.pruneConfigSnapshots()
	return target, nil
}

// ── 采集与应用主进程持有的那部分设置 ──

func mcpScopeToOmap(s mcpScope) *omap {
	ids := make([]any, len(s.AllowedCollections))
	for i, id := range s.AllowedCollections {
		ids[i] = id
	}
	return newOmap().set("mode", s.Mode).set("allowedCollectionIds", ids).set("allowUncategorized", s.AllowUncategorized)
}

func (l *Library) collectMainConfigParts() (styles []any, shortcuts *omap, scope *omap) {
	for _, s := range l.readIllustrationStyles() {
		styles = append(styles, s)
	}
	if styles == nil {
		styles = []any{}
	}
	accs, _ := l.currentShortcuts().(*omap)
	modes, _ := l.currentShortcutModes().(*omap)
	if accs == nil {
		accs = newOmap()
	}
	if modes == nil {
		modes = newOmap()
	}
	shortcuts = newOmap().set("accelerators", cloneOmap(accs)).set("modes", cloneOmap(modes))
	return styles, shortcuts, mcpScopeToOmap(readMcpScope(l.configDir))
}

// selectionFlag 读取 selection.<key>：null 抛 TypeError（与 JS 一致），非对象取任何属性都是 undefined。
func selectionIsFalse(sel any, key string) (bool, error) {
	if sel == nil {
		return false, &jsTypeError{"Cannot read properties of null (reading '" + key + "')"}
	}
	m, ok := sel.(*omap)
	if !ok {
		return false, nil
	}
	b, ok := m.vals[key].(bool)
	return ok && !b, nil
}

type appliedMainConfig struct {
	providers []any
	models    []any
	routes    *omap
	warnings  []any
}

func omapsToAny(ms []*omap) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

func (l *Library) applyMainConfigParts(f *configTransferFile, selection any) (*appliedMainConfig, error) {
	cur, err := l.readAIConfig()
	if err != nil {
		return nil, err
	}
	settingsOff, err := selectionIsFalse(selection, "settings")
	if err != nil {
		return nil, err
	}
	var rec aiReconcileResult
	if settingsOff {
		rec = aiReconcileResult{providers: omapsToAny(cur.Providers), models: omapsToAny(cur.Models), routes: cur.Routes}
	} else {
		rec = reconcileImportedAIConfig(f.Settings.vals["aiProviders"], f.Settings.vals["aiModels"], f.Settings.vals["modelRouteDefaults"], cur)
		if err := l.replaceAIConfig(rec.providers, rec.models, rec.routes); err != nil {
			return nil, err
		}
	}
	warnings := append([]any{}, rec.warnings...)

	stylesOff, _ := selectionIsFalse(selection, "illustrationStyles")
	if !stylesOff && f.HasStyles && len(f.IllustrationStyles) > 0 {
		if _, msg := l.writeIllustrationStyles(f.IllustrationStyles); msg != "" {
			warnings = append(warnings, "配图风格未能导入："+msg)
		}
	}

	shortcutsOff, _ := selectionIsFalse(selection, "shortcuts")
	if !shortcutsOff && f.Shortcuts != nil {
		if err := l.persistImportedShortcuts(f.Shortcuts.Accelerators, f.Shortcuts.Modes); err != nil {
			warnings = append(warnings, "快捷键未能导入："+err.Error())
		}
	}

	// 只在文件真带了范围时才写：旧版本导出的文件没有这个字段，本机现有范围保持原样
	mcpOff, _ := selectionIsFalse(selection, "mcpScope")
	if !mcpOff && f.McpScope != nil {
		if err := writeMcpScope(l.configDir, *f.McpScope); err != nil {
			warnings = append(warnings, "MCP 可访问范围未能导入："+err.Error())
		} else if f.McpScope.Mode == "selected" {
			warnings = append(warnings, fmt.Sprintf("MCP 可访问范围已收紧到 %d 个合集，请在「设置 → MCP 接入」确认这些合集在本机存在", len(f.McpScope.AllowedCollections)))
		}
	}
	return &appliedMainConfig{providers: rec.providers, models: rec.models, routes: rec.routes, warnings: warnings}, nil
}

// ── IPC ──

func describeFSError(err error, op, path string) string {
	if errors.Is(err, os.ErrNotExist) {
		return "ENOENT: no such file or directory, " + op + " '" + path + "'"
	}
	return err.Error()
}

func readConfigTransferFile(path string) (*configTransferFile, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "读取配置文件失败：" + describeFSError(err, "open", path)
	}
	raw, err := parseOrdered(data)
	if err != nil {
		return nil, "读取配置文件失败：" + err.Error()
	}
	return parseConfigTransferFile(raw)
}

func argOrUndefined(a []json.RawMessage, i int) (any, error) {
	if i >= len(a) {
		return jsUndefined, nil
	}
	return parseOrdered(a[i])
}

func omNullish(v any) bool { return v == nil || v == jsUndefined }

func nullishName(v any) string {
	if v == nil {
		return "null"
	}
	return "undefined"
}

func (l *Library) registerConfigTransfer(b *Bridge) {
	configFilters := []dialogFilter{{"归知配置", []string{"json"}}}

	b.handlers[chConfigExport] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		req, err := argOrUndefined(a, 0)
		if err != nil {
			return nil, err
		}
		reqMap, _ := isPlainRecord(req)
		prop := func(k string) any {
			if reqMap == nil {
				return jsUndefined
			}
			if v, ok := reqMap.vals[k]; ok {
				return v
			}
			return jsUndefined
		}
		fail := func(msg string) (any, error) { return map[string]any{"success": false, "error": msg}, nil }

		var password string
		if !omNullish(req) && vTruthyAny(prop("includeSecrets")) {
			switch pw := prop("password").(type) {
			case string:
				password = jsTrim(pw)
			default:
				if !omNullish(pw) {
					return nil, &jsTypeError{"request.password?.trim is not a function"}
				}
			}
			if password == "" {
				return fail("带 API Key 导出必须设置密码")
			}
		}

		// 采集主进程持有的部分（首次读取配图风格会顺带播种文件，与 TS 一致）
		styles, shortcuts, scope := l.collectMainConfigParts()
		if omNullish(req) {
			return fail("采集配置失败：Cannot read properties of " + nullishName(req) + " (reading 'settings')")
		}
		settingsVal := prop("settings")
		if omNullish(settingsVal) {
			settingsVal = newOmap()
		}
		settings := stripNonPortableSettings(settingsVal)
		file := newOmap().
			set("kind", configTransferKind).
			set("version", float64(configTransferVersion)).
			set("exportedAt", nowIso()).
			set("appVersion", appVersion).
			set("settings", settings)
		if v := prop("settingsVersion"); v != jsUndefined {
			file.set("settingsVersion", v)
		}
		if v := prop("includeUiLayout"); v != false {
			file.set("uiLayout", pickTransferableLocalStorage(prop("uiLayout")))
		}
		if v := prop("includeIllustrationStyles"); v != false {
			file.set("illustrationStyles", styles)
		}
		if v := prop("includeShortcuts"); v != false {
			file.set("shortcuts", shortcuts)
		}
		if v := prop("includeMcpScope"); v != false {
			file.set("mcpScope", scope)
		}

		if vTruthyAny(prop("includeSecrets")) {
			enc, err := encryptConfigSecrets(settings, password)
			if err != nil {
				return fail("采集配置失败：" + err.Error())
			}
			file.set("encryption", enc)
		} else {
			// 清空而不是删除字段：结构保持完整，导入端按「有这个字段但是空的」处理
			mapConfigSecrets(settings, func(string) (string, bool) { return "", true })
		}

		name := "GuiZhi-Config-" + snapshotStamp(time.Now()) + ".json"
		if l.saveFileDialog == nil {
			return fail("当前环境不支持选择保存位置")
		}
		path, err := l.saveFileDialog("导出归知配置", defaultExportPath(name), configFilters)
		if err != nil {
			return nil, err
		}
		if path == "" {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		text, err := stringifyIndent2(file)
		if err != nil {
			return fail("写入配置文件失败：" + err.Error())
		}
		if err := os.WriteFile(path, []byte(text+"\n"), 0o644); err != nil {
			return fail("写入配置文件失败：" + err.Error())
		}
		return map[string]any{"success": true, "filePath": path}, nil
	}

	b.handlers[chConfigRead] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		if l.pickFiles == nil {
			return map[string]any{"success": false, "error": "当前环境不支持选择文件"}, nil
		}
		paths, err := l.pickFiles(false, configFilters)
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		file, msg := readConfigTransferFile(paths[0])
		if file == nil {
			return map[string]any{"success": false, "error": msg}, nil
		}
		l.cfg.mu.Lock()
		l.cfg.lastReadPath, l.cfg.hasRead = paths[0], true
		l.cfg.mu.Unlock()
		return newOmap().set("success", true).set("filePath", paths[0]).set("preview", buildConfigPreview(file)), nil
	}

	b.handlers[chConfigApply] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		pathArg, err := argOrUndefined(a, 0)
		if err != nil {
			return nil, err
		}
		pwArg, err := argOrUndefined(a, 1)
		if err != nil {
			return nil, err
		}
		var selection any = newOmap()
		if len(a) > 2 {
			if v, err := parseOrdered(a[2]); err == nil && v != jsUndefined {
				selection = v
			}
		}
		l.cfg.mu.Lock()
		last, hasRead := l.cfg.lastReadPath, l.cfg.hasRead
		l.cfg.mu.Unlock()
		path, isStr := pathArg.(string)
		if !isStr || path == "" || !hasRead || path != last {
			return map[string]any{"success": false, "error": "请重新选择要导入的配置文件"}, nil
		}
		file, msg := readConfigTransferFile(path)
		if file == nil {
			return map[string]any{"success": false, "error": msg}, nil
		}
		password := ""
		switch pw := pwArg.(type) {
		case string:
			password = jsTrim(pw)
		default:
			if !omNullish(pw) {
				return nil, &jsTypeError{"password?.trim is not a function"}
			}
		}
		dec := decryptConfigSecrets(file, password)
		if !dec.ok {
			res := newOmap().set("success", false)
			if dec.wrongPassword {
				res.set("wrongPassword", true)
			}
			return res.set("error", dec.err), nil
		}

		snapshotDir, snapErr := l.snapshotConfigDir()
		if snapErr != nil {
			// 快照失败不该拦住导入，但要说出来——用户得知道这次没有后悔药
			logAppErrorf("config-transfer", "导入前快照", "%s", snapErr.Error())
			snapshotDir = ""
		}

		failure := func(reason string) (any, error) {
			logAppErrorf("config-transfer", "导入配置", "%s", reason)
			res := newOmap().set("success", false)
			if snapshotDir != "" {
				res.set("error", "导入失败："+reason+"。导入前的配置已备份在 "+snapshotDir)
				res.set("snapshotDir", snapshotDir)
			} else {
				res.set("error", "导入失败："+reason)
			}
			return res, nil
		}
		applied, err := l.applyMainConfigParts(file, selection)
		if err != nil {
			return failure(err.Error())
		}
		settingsOff, err := selectionIsFalse(selection, "settings")
		if err != nil {
			return failure(err.Error())
		}
		settings := newOmap()
		if !settingsOff {
			settings = stripNonPortableSettings(file.Settings)
			settings.set("aiProviders", orEmptyAny(applied.providers))
			settings.set("aiModels", orEmptyAny(applied.models))
			settings.set("modelRouteDefaults", applied.routes)
		}
		warnings := append([]any{}, applied.warnings...)
		if dec.failed > 0 {
			warnings = append(warnings, fmt.Sprintf("有 %d 处密钥解不开，需要手动重填", dec.failed))
		}
		if snapshotDir == "" {
			warnings = append(warnings, "导入前的配置快照没能保存")
		}
		mainSync := newOmap()
		for _, k := range settings.keys {
			if persistedSettingsKeys[k] {
				mainSync.set(k, settings.vals[k])
			}
		}
		uiOff, err := selectionIsFalse(selection, "uiLayout")
		if err != nil {
			return failure(err.Error())
		}
		res := newOmap().set("success", true).set("settings", settings)
		if file.HasSettingsVersion {
			res.set("settingsVersion", file.SettingsVersion)
		}
		if !uiOff {
			res.set("uiLayout", pickTransferableLocalStorage(file.UILayout))
		}
		res.set("mainSyncSettings", mainSync).set("warnings", warnings)
		if snapshotDir != "" {
			res.set("snapshotDir", snapshotDir)
		}
		return res, nil
	}
}

func orEmptyAny(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

func vTruthyAny(v any) bool { return v != jsUndefined && jsTruthy(v) }
