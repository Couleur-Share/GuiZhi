package main

// 应用内更新：对应 apps/desktop/src/main/updater.ts 及其依赖的 electron-updater（NSIS 安装包 + GitHub Releases / 镜像 generic 源）。
//
// 流程与 electron-updater 6.x 一致：
//   检查：官方源先取 releases.atom 与 /releases/latest 定 tag（预览通道按 atom 选 tag），再读 <tag>/latest.yml；
//         镜像源直接读 <镜像地址>/latest.yml。版本比 compareVersions，更高才算有更新。
//   下载：校验和为 sha512(base64)；缓存目录 %LOCALAPPDATA%\<app>-updater\pending，命中且校验通过则直接视为已下载。
//   安装：以 `--updated --force-run` 启动安装包后退出本进程（quitAndInstall(false, true)）。
// 与原实现的有意差异、未覆盖的部分记录在 spikes/MYGO-MIGRATION.md。

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	updaterOwner = "Couleur-Share"
	updaterRepo  = "GuiZhi"
)

type updateRequest struct {
	UseMirror bool
	Channel   string // stable | preview
	Trigger   string
}

// normalizeUpdateOptions 对应同名函数：布尔参数 = 是否用镜像；其余字段缺省/非法回落到 stable、manual。
func normalizeUpdateOptions(raw json.RawMessage) updateRequest {
	req := updateRequest{Channel: "stable", Trigger: "manual"}
	if len(raw) == 0 {
		return req
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return req
	}
	switch x := v.(type) {
	case bool:
		req.UseMirror = x
	case map[string]any:
		req.UseMirror = jsTruthy(x["useMirror"])
		if x["channel"] == "preview" {
			req.Channel = "preview"
		}
		if t, ok := x["trigger"].(string); ok && slices.Contains(updateCheckTriggers, t) {
			req.Trigger = t
		}
	}
	return req
}

func getFeedSuffix(channel, tag string) string {
	if channel == "preview" && tag != "" {
		return "download/" + tag
	}
	return "latest/download"
}

func getMirrorSources(channel, tag string) []string {
	s := getFeedSuffix(channel, tag)
	base := "github.com/" + updaterOwner + "/" + updaterRepo + "/releases/" + s
	return []string{
		"https://ghfast.top/https://" + base,
		"https://gh-proxy.com/https://" + base,
		"https://hub.gitmirror.com/https://" + base,
		"https://cors.isteed.cc/" + base,
	}
}

// ── 更新清单 ──

type manifestFile struct {
	URL    string `yaml:"url"`
	SHA512 string `yaml:"sha512"`
	SHA2   string `yaml:"sha2"`
	Size   int64  `yaml:"size"`
}

type updateManifest struct {
	Version               string         `yaml:"version"`
	Files                 []manifestFile `yaml:"files"`
	Path                  string         `yaml:"path"`
	SHA512                string         `yaml:"sha512"`
	SHA2                  string         `yaml:"sha2"`
	ReleaseDate           string         `yaml:"releaseDate"`
	ReleaseName           string         `yaml:"releaseName"`
	ReleaseNotes          any            `yaml:"releaseNotes"`
	IsAdminRightsRequired bool           `yaml:"isAdminRightsRequired"`
}

// resolvedFile 是已解析成绝对地址的安装包条目。
type resolvedFile struct {
	URL      string
	Info     manifestFile
	FileName string
}

// readManifestNotes 对应同名函数：字符串原样，数组取每项 note 用空行拼接。
func readManifestNotes(notes any) string {
	switch n := notes.(type) {
	case string:
		return n
	case []any:
		var parts []string
		for _, e := range n {
			if m, ok := e.(map[string]any); ok {
				if note, _ := m["note"].(string); note != "" {
					parts = append(parts, note)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

type simpleUpdateInfo struct {
	Version      string `json:"version"`
	ReleaseNotes string `json:"releaseNotes,omitempty"`
	ReleaseDate  string `json:"releaseDate,omitempty"`
}

// ── 更新器 ──

type updaterDeps struct {
	client         *http.Client
	githubBase     string // https://github.com
	apiBase        string // https://api.github.com
	currentVersion func() string
	isDev          func() bool
	cacheDir       string // …\<app>-updater
	changelogPaths func() []string
	platform       string
	push           func(channel string, payload any)
	openExternal   func(string) error
	openPath       func(string) error
	showInFolder   func(string)
	downloadsDir   func() string
	spawnInstaller func(installer string, args []string) error
	quit           func()
	createBackup   func(reason string)
	now            func() time.Time
	progressEvery  time.Duration
	mirrors        func(channel, tag string) []string // 测试注入；缺省为 getMirrorSources
}

type feedContext struct {
	channel string
	tag     string
}

type feedProvider struct {
	baseURL string // 解析相对文件地址的基准（带尾斜杠）
	github  bool
	tag     string
}

type Updater struct {
	d               updaterDeps
	mu              sync.Mutex
	allowPrerelease bool
	lastFeed        feedContext
	lastPercent     float64
	manifest        *updateManifest
	provider        *feedProvider
	installerPath   string
	installerCalled bool
}

func newUpdater(d updaterDeps) *Updater {
	if d.client == nil {
		d.client = &http.Client{Timeout: 0}
	}
	if d.githubBase == "" {
		d.githubBase = "https://github.com"
	}
	if d.apiBase == "" {
		d.apiBase = "https://api.github.com"
	}
	if d.mirrors == nil {
		d.mirrors = getMirrorSources
	}
	if d.now == nil {
		d.now = time.Now
	}
	if d.progressEvery == 0 {
		d.progressEvery = 500 * time.Millisecond
	}
	if d.platform == "" {
		d.platform = nodePlatform()
	}
	u := &Updater{d: d, lastFeed: feedContext{channel: "stable"}}
	u.allowPrerelease = isPrereleaseVersion(d.currentVersion())
	return u
}

func nodePlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

func (u *Updater) send(status map[string]any) { u.d.push("updater:status", status) }

func (u *Updater) logCheck(entry map[string]any) {
	entry["event"] = "updater:check"
	entry["currentVersion"] = u.d.currentVersion()
	if e, ok := entry["error"].(string); ok && len(e) > 300 {
		entry["error"] = string([]rune(e)[:300])
	}
	b, _ := json.Marshal(entry)
	log.Printf("[updater] %s", b)
}

// mapUpdaterError 对应 autoUpdater.on("error") 里的文案归一。
func mapUpdaterError(message string) string {
	switch {
	case strings.Contains(message, "404") || strings.Contains(message, "Cannot find"):
		return "Update check failed: Cannot find update manifest file in the latest release.\n" +
			"更新检查失败：无法在最新版本中找到更新配置文件。\n" +
			"请前往 GitHub Releases 页面手动下载安装。"
	case strings.Contains(message, "ZIP file not provided"):
		return "Auto update requires ZIP installer, but current Release does not have corresponding ZIP file. Please go to GitHub Releases page to download manually, or wait for next version to fix auto update."
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "sha512") && strings.Contains(lower, "mismatch") {
		return "SHA512 校验失败\n\n" +
			"这通常是由于 CDN 缓存不一致或网络问题导致的。\n" +
			"文件可能已下载完成，您可以点击下方按钮打开文件夹手动尝试安装。"
	}
	return message
}

// emitError 对应 autoUpdater 的 error 事件：先推一条状态，调用方再决定返回给渲染进程的文案。
func (u *Updater) emitError(err error) {
	log.Printf("[updater] error: %v", err)
	u.send(map[string]any{"status": "error", "error": mapUpdaterError(err.Error())})
}

// ── HTTP ──

type httpStatusError struct {
	status int
	text   string
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("%d %q", e.status, e.text) }

func (u *Updater) get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := u.d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{resp.StatusCode, http.StatusText(resp.StatusCode)}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// ── 官方 GitHub 源 ──

type atomEntry struct {
	Title string `xml:"title"`
	Link  struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Content string `xml:"content"`
}

var reTagHref = regexp.MustCompile(`/tag/(v?[^/]+)$`)

func entryTag(e atomEntry) (string, bool) {
	m := reTagHref.FindStringSubmatch(e.Link.Href)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func prereleaseChannelOf(v string) string {
	p := parseVersion(v)
	if p == nil || len(p.prerelease) == 0 {
		return ""
	}
	return fmt.Sprint(p.prerelease[0])
}

func (u *Updater) githubLatest(ctx context.Context, allowPrerelease bool) (*updateManifest, *feedProvider, error) {
	basePath := "/" + updaterOwner + "/" + updaterRepo + "/releases"
	feedXML, err := u.get(ctx, u.d.githubBase+basePath+".atom", map[string]string{"Accept": "application/xml, application/atom+xml, text/xml, */*"})
	if err != nil {
		return nil, nil, err
	}
	var feed struct {
		Entries []atomEntry `xml:"entry"`
	}
	if err := xml.Unmarshal(feedXML, &feed); err != nil {
		return nil, nil, fmt.Errorf("Cannot parse releases feed: %v,\nXML:\n%s", err, feedXML)
	}
	if len(feed.Entries) == 0 {
		return nil, nil, errors.New("No published versions on GitHub")
	}
	latest := feed.Entries[0]
	tag := ""
	if allowPrerelease {
		current := prereleaseChannelOf(u.d.currentVersion())
		if current == "" {
			// 当前是正式版且允许预发布：Atom 里第一条（可能是预发布）
			tag, _ = entryTag(latest)
		} else {
			for _, e := range feed.Entries {
				hrefTag, ok := entryTag(e)
				if !ok || parseVersion(hrefTag) == nil {
					continue
				}
				hrefChannel := prereleaseChannelOf(hrefTag)
				shouldFetch := current == "alpha" || current == "beta"
				isCustom := hrefChannel != "" && hrefChannel != "alpha" && hrefChannel != "beta"
				mismatch := current == "beta" && hrefChannel == "alpha"
				if shouldFetch && !isCustom && !mismatch {
					tag, latest = hrefTag, e
					break
				}
				if hrefChannel != "" && hrefChannel == current {
					tag, latest = hrefTag, e
					break
				}
			}
		}
	} else {
		body, err := u.get(ctx, u.d.githubBase+basePath+"/latest", map[string]string{"Accept": "application/json"})
		if err != nil {
			return nil, nil, fmt.Errorf("Unable to find latest version on GitHub (%s%s/latest), please ensure a production release exists: %v", u.d.githubBase, basePath, err)
		}
		var info struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &info); err != nil {
			return nil, nil, fmt.Errorf("Unable to find latest version on GitHub: %v", err)
		}
		tag = info.TagName
		for _, e := range feed.Entries {
			if t, ok := entryTag(e); ok && t == tag {
				latest = e
				break
			}
		}
	}
	if tag == "" {
		return nil, nil, errors.New("No published versions on GitHub")
	}
	fetch := func(channel string) (*updateManifest, error) {
		file := channel + ".yml"
		fileURL := u.d.githubBase + basePath + "/download/" + tag + "/" + file
		raw, err := u.get(ctx, fileURL, nil)
		if err != nil {
			var he *httpStatusError
			if errors.As(err, &he) && he.status == 404 {
				return nil, fmt.Errorf("Cannot find %s in the latest release artifacts (%s): %v", file, fileURL, err)
			}
			return nil, err
		}
		return parseUpdateManifest(raw, file, fileURL)
	}
	channel := "latest"
	if allowPrerelease {
		if id := prereleaseChannelOf(tag); id != "" {
			channel = id
		}
	}
	m, err := fetch(channel)
	if err != nil {
		if !allowPrerelease {
			return nil, nil, err
		}
		if m, err = fetch("latest"); err != nil { // 回退到 latest.yml
			return nil, nil, err
		}
	}
	if m.ReleaseName == "" {
		m.ReleaseName = latest.Title
	}
	if m.ReleaseNotes == nil {
		if latest.Content != "No content." {
			m.ReleaseNotes = latest.Content
		} else {
			m.ReleaseNotes = ""
		}
	}
	return m, &feedProvider{baseURL: u.d.githubBase + basePath + "/download/" + tag + "/", github: true, tag: tag}, nil
}

func parseUpdateManifest(raw []byte, file, fileURL string) (*updateManifest, error) {
	var m updateManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("Cannot parse update info from %s in the latest release artifacts (%s): %v, rawData: %s", file, fileURL, err, raw)
	}
	return &m, nil
}

// ── 镜像（generic）源 ──

func (u *Updater) genericLatest(ctx context.Context, feedURL string) (*updateManifest, *feedProvider, error) {
	base := strings.TrimRight(feedURL, "/") + "/"
	fileURL := base + "latest.yml"
	raw, err := u.get(ctx, fileURL, nil)
	if err != nil {
		var he *httpStatusError
		if errors.As(err, &he) && he.status == 404 {
			return nil, nil, fmt.Errorf("Cannot find latest.yml in the latest release artifacts (%s): %v", fileURL, err)
		}
		return nil, nil, err
	}
	m, err := parseUpdateManifest(raw, "latest.yml", fileURL)
	if err != nil {
		return nil, nil, err
	}
	return m, &feedProvider{baseURL: base}, nil
}

// resolveFiles 对应 Provider.resolveFiles：校验和必须有；GitHub 源里文件名的空格换成 `-`。
func (p *feedProvider) resolveFiles(m *updateManifest) ([]resolvedFile, error) {
	files := m.Files
	if len(files) == 0 {
		if m.Path == "" {
			return nil, errors.New("No files provided")
		}
		files = []manifestFile{{URL: m.Path, SHA2: m.SHA2, SHA512: m.SHA512}}
	}
	base, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, err
	}
	var out []resolvedFile
	for _, f := range files {
		if f.SHA2 == "" && f.SHA512 == "" {
			return nil, fmt.Errorf("Update info doesn't contain nor sha256 neither sha512 checksum: %+v", f)
		}
		name := f.URL
		if p.github {
			name = strings.ReplaceAll(name, " ", "-")
		}
		ref, err := url.Parse(name)
		if err != nil {
			return nil, err
		}
		abs := base.ResolveReference(ref)
		out = append(out, resolvedFile{URL: abs.String(), Info: f, FileName: path.Base(abs.Path)})
	}
	return out, nil
}

// findInstaller 对应 findFile(files, "exe")：只看 .exe，优先文件名含当前架构的。
func findInstaller(files []resolvedFile) (resolvedFile, error) {
	if len(files) == 0 {
		return resolvedFile{}, errors.New("No files provided")
	}
	var exes []resolvedFile
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.FileName), ".exe") {
			exes = append(exes, f)
		}
	}
	arch := "x64"
	for _, f := range exes {
		if strings.Contains(f.URL, arch) || strings.Contains(f.Info.URL, arch) {
			return f, nil
		}
	}
	if len(exes) > 0 {
		return exes[0], nil
	}
	return files[0], nil
}

// ── 检查 ──

func (u *Updater) toSimpleInfo(m *updateManifest) simpleUpdateInfo {
	cur := u.d.currentVersion()
	notes := u.changelogForRange(m.Version, cur)
	if notes == "" {
		manifest := readManifestNotes(m.ReleaseNotes)
		if len(parseChangelogVersions(manifest)) > 1 {
			if notes = extractChangelogRange(manifest, m.Version, cur); notes == "" {
				notes = extractLatestChangelogSection(manifest)
			}
		} else {
			notes = manifest
		}
	}
	return simpleUpdateInfo{Version: m.Version, ReleaseNotes: notes, ReleaseDate: m.ReleaseDate}
}

// changelogForRange 对应 getChangelogForVersionRange：随包的 CHANGELOG.md 里 (current, new] 区间。
func (u *Updater) changelogForRange(newVersion, currentVersion string) string {
	if u.d.changelogPaths == nil {
		return ""
	}
	for _, p := range u.d.changelogPaths() {
		if raw, err := os.ReadFile(p); err == nil {
			return extractChangelogRange(string(raw), newVersion, currentVersion)
		}
	}
	return ""
}

func (u *Updater) resolveFeedContext(ctx context.Context, channel string) (feedContext, error) {
	if channel != "preview" {
		return feedContext{channel: channel}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, err := u.get(ctx, u.d.apiBase+"/repos/"+updaterOwner+"/"+updaterRepo+"/releases?per_page=20", map[string]string{
		"Accept": "application/vnd.github+json", "User-Agent": "GuiZhi-Updater",
	})
	if err != nil {
		var he *httpStatusError
		if errors.As(err, &he) {
			return feedContext{}, fmt.Errorf("Preview release lookup failed with HTTP %d", he.status)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return feedContext{}, errors.New("Preview release lookup timed out")
		}
		return feedContext{}, err
	}
	var releases []githubReleaseCandidate
	if err := json.Unmarshal(body, &releases); err != nil {
		return feedContext{}, err
	}
	choice := pickPreviewFeedRelease(releases)
	if choice == nil {
		return feedContext{}, errors.New("Update check failed: No published release is currently available.")
	}
	return feedContext{channel: channel, tag: releaseTagForPreviewFeed(choice)}, nil
}

type checkOutcome struct {
	manifest  *updateManifest
	available bool
}

// checkOnce 对应 autoUpdater.checkForUpdates()：推送 checking → available / not-available，失败推送 error。
func (u *Updater) checkOnce(ctx context.Context, feedURL string, official bool, allowPrerelease bool) (*checkOutcome, error) {
	u.send(map[string]any{"status": "checking"})
	var (
		m   *updateManifest
		p   *feedProvider
		err error
	)
	if official {
		m, p, err = u.githubLatest(ctx, allowPrerelease)
	} else {
		m, p, err = u.genericLatest(ctx, feedURL)
	}
	if err != nil {
		u.emitError(err)
		return nil, err
	}
	if m.Version == "" {
		err = errors.New("Cannot parse update info: version is missing")
		u.emitError(err)
		return nil, err
	}
	u.mu.Lock()
	u.manifest, u.provider = m, p
	u.mu.Unlock()
	available := compareVersions(m.Version, u.d.currentVersion()) > 0
	st := "not-available"
	if available {
		st = "available"
	}
	u.send(map[string]any{"status": st, "info": u.toSimpleInfo(m)})
	return &checkOutcome{manifest: m, available: available}, nil
}

func checkResultJSON(o *checkOutcome) map[string]any {
	return map[string]any{
		"success": true, "updateAvailable": o.available,
		"result": map[string]any{
			"isUpdateAvailable": o.available,
			"updateInfo":        map[string]any{"version": o.manifest.Version, "releaseDate": o.manifest.ReleaseDate},
		},
	}
}

func (u *Updater) Check(ctx context.Context, raw json.RawMessage) map[string]any {
	req := normalizeUpdateOptions(raw)
	if u.d.isDev() {
		u.logCheck(map[string]any{"trigger": req.Trigger, "channel": req.Channel, "result": "dev-disabled"})
		return map[string]any{"success": false, "devDisabled": true, "error": "Update check disabled in development mode"}
	}
	started := u.d.now()
	u.mu.Lock()
	u.allowPrerelease = req.Channel == "preview"
	allow := u.allowPrerelease
	u.mu.Unlock()
	fail := func(feed feedContext, msg string) map[string]any {
		u.logCheck(map[string]any{"trigger": req.Trigger, "channel": feed.channel, "useMirror": req.UseMirror, "result": "error", "durationMs": u.d.now().Sub(started).Milliseconds(), "error": msg})
		return map[string]any{"success": false, "error": msg}
	}
	feed, err := u.resolveFeedContext(ctx, req.Channel)
	if err != nil {
		return fail(feedContext{channel: req.Channel}, err.Error())
	}
	u.mu.Lock()
	u.lastFeed = feed
	u.mu.Unlock()
	record := func(o *checkOutcome) map[string]any {
		result := "not-available"
		if o.available {
			result = "available"
		}
		u.logCheck(map[string]any{"trigger": req.Trigger, "channel": feed.channel, "useMirror": req.UseMirror, "result": result, "version": o.manifest.Version, "durationMs": u.d.now().Sub(started).Milliseconds()})
		return checkResultJSON(o)
	}
	if req.UseMirror {
		for _, mirror := range u.d.mirrors(feed.channel, feed.tag) {
			log.Printf("[updater] Using %s mirror for check: %s", feed.channel, mirror)
			if o, err := u.checkOnce(ctx, mirror, false, allow); err == nil {
				return record(o)
			}
		}
		return fail(feed, "All mirror sources failed. Please try disabling mirror acceleration.")
	}
	o, err := u.checkOnce(ctx, "", true, allow)
	if err != nil {
		return fail(feed, "Update check failed: "+err.Error())
	}
	return record(o)
}

// ── 下载 ──

func (u *Updater) pendingDir() string { return filepath.Join(u.d.cacheDir, "pending") }

type cachedUpdateInfo struct {
	FileName              string `json:"fileName"`
	SHA512                string `json:"sha512"`
	IsAdminRightsRequired bool   `json:"isAdminRightsRequired"`
}

func fileSHA512(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

func (u *Updater) emitDownloaded(m *updateManifest, installer string) {
	u.mu.Lock()
	u.installerPath = installer
	u.mu.Unlock()
	u.send(map[string]any{"status": "downloaded", "info": u.toSimpleInfo(m)})
}

// downloadUpdate 对应 autoUpdater.downloadUpdate()：使用上一次检查得到的清单与源（镜像检查的结果就从镜像下）。
func (u *Updater) downloadUpdate(ctx context.Context) error {
	u.mu.Lock()
	m, p := u.manifest, u.provider
	u.mu.Unlock()
	if m == nil || p == nil {
		err := errors.New("Please check update first")
		u.emitError(err)
		return err
	}
	fail := func(err error) error { u.emitError(err); return err }
	files, err := p.resolveFiles(m)
	if err != nil {
		return fail(err)
	}
	file, err := findInstaller(files)
	if err != nil {
		return fail(err)
	}
	expected := file.Info.SHA512
	dest := filepath.Join(u.pendingDir(), file.FileName)
	infoFile := filepath.Join(u.pendingDir(), "update-info.json")

	// 缓存命中：上次已下载同一个包（元数据与校验和都对得上）
	if raw, err := os.ReadFile(infoFile); err == nil && expected != "" {
		var ci cachedUpdateInfo
		if json.Unmarshal(raw, &ci) == nil && ci.SHA512 == expected {
			if sum, err := fileSHA512(dest); err == nil && sum == expected {
				log.Printf("[updater] Update has already been downloaded to %s", dest)
				u.emitDownloaded(m, dest)
				return nil
			}
		}
	}
	_ = os.RemoveAll(u.pendingDir())
	if err := os.MkdirAll(u.pendingDir(), 0o755); err != nil {
		return fail(err)
	}
	tmp := dest + ".part"
	if err := u.downloadTo(ctx, file, tmp); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	if expected != "" {
		sum, err := fileSHA512(tmp)
		if err != nil {
			os.Remove(tmp)
			return fail(err)
		}
		if sum != expected {
			os.Remove(tmp)
			return fail(fmt.Errorf("sha512 checksum mismatch, expected %s, got %s", expected, sum))
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fail(err)
	}
	ci, _ := json.Marshal(cachedUpdateInfo{FileName: file.FileName, SHA512: expected, IsAdminRightsRequired: m.IsAdminRightsRequired})
	_ = os.WriteFile(infoFile, ci, 0o644)
	u.emitDownloaded(m, dest)
	return nil
}

func (u *Updater) downloadTo(ctx context.Context, file resolvedFile, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return err
	}
	resp, err := u.d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{resp.StatusCode, http.StatusText(resp.StatusCode)}
	}
	total := resp.ContentLength
	if total <= 0 {
		total = file.Info.Size
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	var transferred int64
	start := u.d.now()
	last := start
	report := func(final bool) {
		now := u.d.now()
		if !final && now.Sub(last) < u.d.progressEvery {
			return
		}
		last = now
		percent := 0.0
		if total > 0 {
			percent = float64(transferred) / float64(total) * 100
		}
		bps := 0.0
		if el := now.Sub(start).Seconds(); el > 0 {
			bps = float64(transferred) / el
		}
		u.reportProgress(percent, bps, total, transferred)
	}
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			transferred += int64(n)
			report(false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	report(true)
	return out.Close()
}

// reportProgress 对应 download-progress 处理：进度不允许回退（除非已接近完成）。
func (u *Updater) reportProgress(percent, bps float64, total, transferred int64) {
	u.mu.Lock()
	if percent < u.lastPercent && u.lastPercent < 99 {
		u.mu.Unlock()
		return
	}
	u.lastPercent = percent
	u.mu.Unlock()
	u.send(map[string]any{"status": "downloading", "progress": map[string]any{"percent": percent, "bytesPerSecond": bps, "total": total, "transferred": transferred}})
}

func (u *Updater) Download(ctx context.Context, raw json.RawMessage) map[string]any {
	if u.d.isDev() {
		return map[string]any{"success": false, "error": "Download disabled in development mode"}
	}
	req := normalizeUpdateOptions(raw)
	u.mu.Lock()
	u.allowPrerelease = req.Channel == "preview"
	u.lastPercent = 0
	last := u.lastFeed
	u.mu.Unlock()
	if last.channel != req.Channel {
		feed, err := u.resolveFeedContext(ctx, req.Channel)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}
		}
		u.mu.Lock()
		u.lastFeed = feed
		u.mu.Unlock()
	}
	if err := u.downloadUpdate(ctx); err != nil {
		if req.UseMirror {
			return map[string]any{"success": false, "error": "All mirror sources failed. Please try disabling mirror acceleration."}
		}
		return map[string]any{"success": false, "error": "Download update failed: " + err.Error()}
	}
	return map[string]any{"success": true}
}

// ── 安装 / 打开 ──

func (u *Updater) Install() map[string]any {
	if u.d.isDev() {
		return map[string]any{"success": false, "error": "Install disabled in development mode"}
	}
	u.mu.Lock()
	installer, called := u.installerPath, u.installerCalled
	u.mu.Unlock()
	if called {
		return map[string]any{"success": false, "error": "Install update failed: install call ignored: already started"}
	}
	if installer == "" {
		// electron-updater 在这里只推送 error 事件并静默返回，TS 的 handler 仍报成功；这里如实返回失败（有意差异）。
		err := errors.New("No update filepath provided, can't quit and install")
		u.emitError(err)
		return map[string]any{"success": false, "error": "Install update failed: " + err.Error()}
	}
	if u.d.createBackup != nil {
		u.d.createBackup("pre-update")
	}
	u.mu.Lock()
	u.installerCalled = true
	u.mu.Unlock()
	args := []string{"--updated", "--force-run"}
	if err := u.d.spawnInstaller(installer, args); err != nil {
		// 对应 EACCES/ENOENT 的回退：交给系统打开（会按需弹出 UAC）
		log.Printf("[updater] Cannot run installer (%v)，改用系统打开", err)
		if err2 := u.d.openPath(installer); err2 != nil {
			u.mu.Lock()
			u.installerCalled = false
			u.mu.Unlock()
			u.emitError(err2)
			return map[string]any{"success": false, "error": "Install update failed: " + err2.Error()}
		}
	}
	go func() {
		time.Sleep(50 * time.Millisecond) // 对应 setImmediate：先让返回值送达渲染进程
		u.d.quit()
	}()
	return map[string]any{"success": true, "manual": false}
}

func (u *Updater) OpenDownloaded() map[string]any {
	u.mu.Lock()
	installer := u.installerPath
	u.mu.Unlock()
	if installer != "" {
		if _, err := os.Stat(installer); err == nil {
			u.d.showInFolder(installer)
			return map[string]any{"success": true, "path": installer}
		}
	}
	dir := u.d.downloadsDir()
	openErr := u.d.openPath(dir)
	base := "No downloaded update file is available"
	if installer != "" {
		base = "Downloaded update file is missing"
	}
	msg := base
	if openErr != nil && strings.TrimSpace(openErr.Error()) != "" {
		msg = base + "; failed to open Downloads folder: " + openErr.Error()
	}
	return map[string]any{"success": false, "path": dir, "error": msg}
}

// ── IPC 注册 ──

func (u *Updater) register(b *Bridge) {
	rawArg := func(a []json.RawMessage) json.RawMessage {
		if len(a) == 0 {
			return nil
		}
		return a[0]
	}
	b.handlers["updater:installSource"] = func(context.Context, []json.RawMessage) (any, error) { return "unknown", nil }
	b.handlers["updater:platform"] = func(context.Context, []json.RawMessage) (any, error) { return u.d.platform, nil }
	b.handlers["updater:check"] = func(ctx context.Context, a []json.RawMessage) (any, error) { return u.Check(ctx, rawArg(a)), nil }
	b.handlers["updater:download"] = func(ctx context.Context, a []json.RawMessage) (any, error) { return u.Download(ctx, rawArg(a)), nil }
	b.handlers["updater:install"] = func(context.Context, []json.RawMessage) (any, error) { return u.Install(), nil }
	b.handlers["updater:openDownloadedUpdate"] = func(context.Context, []json.RawMessage) (any, error) { return u.OpenDownloaded(), nil }
	b.handlers["updater:openReleases"] = func(context.Context, []json.RawMessage) (any, error) {
		if err := u.d.openExternal("https://github.com/" + updaterOwner + "/" + updaterRepo + "/releases"); err != nil {
			logAppErrorf("updater", "打开 Releases 页面", "%s", err.Error())
		}
		return nil, nil
	}
}
