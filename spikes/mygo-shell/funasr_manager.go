package main

// 对应 services/media/funasr-manager.ts（Windows Python 形态）：本地转写引擎的托管安装、状态与卸载。
//
//	Python 运行时（官方 python-build-standalone，校验 SHA-256）→ venv 装 funasr/torch → 首次启动下载模型
//	→ 写入内置模型并把「语音转写」路由指过来。
//
// 与 TS 的差异（见 MYGO-MIGRATION.md）：tar.gz 由 Go 原生解包（拒绝越界/链接条目），不再调用系统 tar；
// macOS arm64 的 GGUF 形态不在 Go 壳内安装与拉起（平台政策只支持 Windows x64）。

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	pythonReleaseTag   = "20260610"
	pythonAssetName    = "cpython-3.12.13+20260610-x86_64-pc-windows-msvc-install_only.tar.gz"
	pipIndexURLInstall = "https://pypi.tuna.tsinghua.edu.cn/simple"

	funasrVenvTimeout    = 3 * time.Minute
	funasrExtractTimeout = 5 * time.Minute
	funasrDepsTimeout    = 30 * time.Minute
	funasrModelBootWait  = 20 * time.Minute
	depsProgressThrottle = 500 * time.Millisecond
	outputTailMax        = 4096
)

var pipPackages = []string{"funasr", "fastapi", "uvicorn", "python-multipart", "torch", "torchaudio"}

func withGithubMirrors(official string) []string {
	return []string{official, "https://ghfast.top/" + official, "https://gh-proxy.com/" + official, "https://hub.gitmirror.com/" + official}
}

func pythonDownloadOfficial() string {
	return "https://github.com/astral-sh/python-build-standalone/releases/download/" + pythonReleaseTag + "/" + strings.Replace(pythonAssetName, "+", "%2B", 1)
}

func pythonChecksumOfficial() string {
	return "https://github.com/astral-sh/python-build-standalone/releases/download/" + pythonReleaseTag + "/SHA256SUMS"
}

func getPythonDownloadURLs(platform string) []string {
	if platform != "win32" {
		return nil
	}
	return withGithubMirrors(pythonDownloadOfficial())
}

// getPythonChecksumURLs 官方发布的 SHA256SUMS；优先官方源，镜像只作兜底。
func getPythonChecksumURLs() []string { return withGithubMirrors(pythonChecksumOfficial()) }

func isFunasrInstallSupported(platform string) bool { return platform == "win32" }

func resolveInstallFlavor(platform string) string {
	if platform == "win32" {
		return funasrFlavorPython
	}
	return ""
}

// ── 外部命令 ──

type funasrToolOptions struct {
	Timeout  time.Duration
	OnOutput func(line string)
	Env      []string // nil 表示继承当前进程环境
}

// funasrToolRunner 对应 RunFunasrTool：收集输出、超时终止、失败附错误尾部。可注入替身（测试与更新事务）。
type funasrToolRunner func(executable string, args []string, opts funasrToolOptions) (string, error)

var reNewline = regexp.MustCompile(`\r?\n`)

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if isWindows() {
		k := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid))
		hideChildWindow(k)
		if k.Run() == nil {
			return
		}
	}
	_ = cmd.Process.Kill()
}

func runFunasrToolProcess(executable string, args []string, opts funasrToolOptions) (string, error) {
	cmd := exec.Command(executable, args...)
	hideChildWindow(cmd)
	cmd.Env = opts.Env
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	timeout := orDuration(opts.Timeout, funasrToolTimeout)
	var (
		mu       sync.Mutex
		stdout   string
		tail     string
		timedOut bool
	)
	handle := func(chunk []byte, isStdout bool) {
		text := string(chunk)
		mu.Lock()
		if isStdout && len(stdout) < outputTailMax*4 {
			stdout += text
		}
		tail = utf16Tail(tail+text, outputTailMax)
		mu.Unlock()
		if opts.OnOutput != nil {
			var last string
			for _, ln := range reNewline.Split(text, -1) {
				if t := trimJS(ln); t != "" {
					last = t
				}
			}
			if last != "" {
				opts.OnOutput(last)
			}
		}
	}
	var wg sync.WaitGroup
	for i, r := range []io.Reader{stdoutPipe, stderrPipe} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 16*1024)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					handle(buf[:n], i == 0)
				}
				if err != nil {
					return
				}
			}
		}()
	}
	timer := time.AfterFunc(timeout, func() {
		mu.Lock()
		timedOut = true
		mu.Unlock()
		killProcessTree(cmd) // 等 Wait 返回后才算结束，避免 pip / 构建子进程继续写入恢复后的环境
	})
	wg.Wait()
	waitErr := cmd.Wait()
	timer.Stop()
	mu.Lock()
	defer mu.Unlock()
	switch {
	case timedOut:
		return "", fmt.Errorf("%s 执行超时", filepath.Base(executable))
	case waitErr == nil:
		return stdout, nil
	}
	code := -1
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		code = ee.ExitCode()
	}
	return "", errors.New(explainFunasrProxyFailure(fmt.Sprintf("%s 退出码 %d: %s", filepath.Base(executable), code, utf16Tail(trimJS(tail), 300))))
}

func (e *funasrEngine) tool() funasrToolRunner {
	if e.runTool != nil {
		return e.runTool
	}
	return runFunasrToolProcess
}

// removeDirWithRetry 带重试的目录删除（Windows 上刚结束的进程可能短暂占用文件）。
func (e *funasrEngine) removeDirWithRetry(dir string, attempts int) error {
	for i := 0; i < attempts; i++ {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}
		if i == attempts-1 {
			return err
		}
		time.Sleep(orDuration(e.removeRetryDelay, time.Second))
	}
	return nil
}

// ── 下载 ──

type funasrInstallProgress struct {
	Phase   string `json:"phase"`
	Percent *int   `json:"percent"`
	Detail  string `json:"detail,omitempty"`
}

func progressPhase(phase string) funasrInstallProgress { return funasrInstallProgress{Phase: phase} }

// downloadFirstOk 按顺序尝试各下载源；校验和不符同样视为该源失败。
func (e *funasrEngine) downloadFirstOk(ctx context.Context, urls []string, target, expectedSha256, phase string, onProgress func(funasrInstallProgress)) error {
	var failures []string
	for _, u := range urls {
		err := func() error {
			fmt.Printf("[funasr] 开始下载: %s\n", u)
			if err := e.l.downloadToFile(ctx, u, target, func(p toolDownloadProgress) {
				if onProgress == nil {
					return
				}
				pr := funasrInstallProgress{Phase: phase, Detail: fmt.Sprintf("%.1f MB", float64(p.Transferred)/(1024*1024))}
				if p.Total != nil && *p.Total > 0 {
					pct := min(100, int(float64(p.Transferred)/float64(*p.Total)*100+0.5))
					pr.Percent = &pct
				}
				onProgress(pr)
			}); err != nil {
				return err
			}
			if expectedSha256 != "" {
				actual, err := sha256File(target)
				if err != nil {
					return err
				}
				if actual != expectedSha256 {
					return fmt.Errorf("校验和不匹配（期望 %s…，实际 %s…）", expectedSha256[:12], actual[:12])
				}
			}
			return nil
		}()
		if err == nil {
			return nil
		}
		host := hostOf(u)
		failures = append(failures, host+": "+err.Error())
		fmt.Printf("[funasr] 下载源失败（%s），尝试下一个\n", host)
		_ = os.Remove(target)
	}
	return errors.New("全部下载源失败——" + strings.Join(failures, "；"))
}

// extractTarGz 原生解包：拒绝绝对路径与越界条目；符号链接跳过；硬链接按已解出的目标复制内容。
func extractTarGz(ctx context.Context, archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	resolve := func(name string) (string, error) {
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if clean == "." {
			return absDest, nil
		}
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
			return "", fmt.Errorf("压缩包含有越界路径：%s", name)
		}
		target := filepath.Join(absDest, filepath.FromSlash(clean))
		if target != absDest && !strings.HasPrefix(target, absDest+string(filepath.Separator)) {
			return "", fmt.Errorf("压缩包含有越界路径：%s", name)
		}
		return target, nil
	}
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := resolve(h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeLink:
			src, err := resolve(h.Linkname)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("硬链接目标缺失：%s", h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0o755); err != nil {
				return err
			}
		}
	}
}

// ── 状态 ──

type funasrStatus struct {
	Installed        bool   `json:"installed"`
	Running          bool   `json:"running"`
	Port             int    `json:"port"`
	Dir              string `json:"dir"`
	Version          string `json:"version,omitempty"`
	InstallSupported bool   `json:"installSupported"`
	UpdateSupported  bool   `json:"updateSupported"`
	InstallFlavor    string `json:"installFlavor,omitempty"`
}

func (e *funasrEngine) status() funasrStatus {
	paths := e.l.funasrPaths()
	installed := isFunasrInstalled(paths)
	running := installed && e.probeHealth(3*time.Second)
	platform := e.platformOr()
	flavor := resolveFunasrEngineFlavor(paths)
	if flavor == "" {
		flavor = resolveInstallFlavor(platform)
	}
	st := funasrStatus{
		Installed: installed, Running: running, Port: e.portNum(), Dir: paths.Root,
		InstallSupported: isFunasrInstallSupported(platform),
		UpdateSupported:  platform == "win32" && flavor == funasrFlavorPython,
		InstallFlavor:    flavor,
	}
	if s := readFunasrState(paths); s != nil {
		st.Version = s.FunasrVersion
	}
	return st
}

// ── AI 配置接入 ──

const funasrBaseURL = "http://127.0.0.1:8620/v1"

// upsertBuiltinTranscription 写入 / 更新内置服务商与模型条目，并把「语音转写」路由指向本地引擎。
func upsertBuiltinTranscription(cfg *aiConfigFile, verifiedAt string) (providers, models []any, routes *omap) {
	provider := newOmap().set("id", funasrProviderID).set("name", "本地转写引擎").set("provider", "custom").
		set("apiProtocol", "openai").set("apiKey", "local").set("apiUrl", funasrBaseURL).set("lastVerifiedAt", verifiedAt)
	isDefault := false
	existing := false
	for _, m := range cfg.Models {
		if m.vals["id"] == funasrModelID {
			existing = true
			isDefault = m.vals["isDefault"] == true
		}
	}
	model := newOmap().set("id", funasrModelID).set("name", "SenseVoiceSmall（本地）").set("providerId", funasrProviderID).
		set("provider", "custom").set("apiProtocol", "openai").set("apiKey", "local").set("apiUrl", funasrBaseURL).
		set("model", funasrModelName).set("isDefault", isDefault).set("lastVerifiedAt", verifiedAt).
		set("capabilities", newOmap().set("chat", false).set("audioTranscription", true))

	hasProvider := false
	for _, p := range cfg.Providers {
		if p.vals["id"] == funasrProviderID {
			providers = append(providers, provider)
			hasProvider = true
		} else {
			providers = append(providers, p)
		}
	}
	if !hasProvider {
		providers = append(providers, provider)
	}
	for _, m := range cfg.Models {
		if m.vals["id"] == funasrModelID {
			models = append(models, model)
		} else {
			models = append(models, m)
		}
	}
	if !existing {
		models = append(models, model)
	}
	routes = newOmap()
	if cfg.Routes != nil {
		for _, k := range cfg.Routes.keys {
			routes.set(k, cfg.Routes.vals[k])
		}
	}
	routes.set("audioText", funasrModelID)
	return providers, models, routes
}

// removeBuiltinTranscription 移除内置条目；「语音转写」路由仅在仍指向本地引擎时清除。
func removeBuiltinTranscription(cfg *aiConfigFile) (providers, models []any, routes *omap) {
	for _, p := range cfg.Providers {
		if p.vals["id"] != funasrProviderID {
			providers = append(providers, p)
		}
	}
	for _, m := range cfg.Models {
		if m.vals["id"] != funasrModelID {
			models = append(models, m)
		}
	}
	routes = newOmap()
	if cfg.Routes != nil {
		for _, k := range cfg.Routes.keys {
			if k == "audioText" && cfg.Routes.vals[k] == funasrModelID {
				continue
			}
			routes.set(k, cfg.Routes.vals[k])
		}
	}
	return providers, models, routes
}

func (l *Library) writeBuiltinTranscription(remove bool) error {
	cfg, err := l.readAIConfig()
	if err != nil {
		return err
	}
	var providers, models []any
	var routes *omap
	if remove {
		providers, models, routes = removeBuiltinTranscription(cfg)
	} else {
		providers, models, routes = upsertBuiltinTranscription(cfg, nowIso())
	}
	if providers == nil {
		providers = []any{}
	}
	if models == nil {
		models = []any{}
	}
	return l.replaceAIConfig(providers, models, routes)
}

// ── 安装 / 卸载 ──

// funasrSocksBlocked：pip 无法经 SOCKS 代理访问索引；必须先于停服务、清理旧环境和下载运行时检查。
func funasrSocksBlocked(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return activeNetProxy.ProxyFor(u) != nil && activeNetProxy.Settings().Protocol == "socks5"
}

var reVersionLine = regexp.MustCompile(`(?m)^Version:\s*(.+)$`)

func (e *funasrEngine) installWindowsPython(ctx context.Context, onProgress func(funasrInstallProgress)) (string, error) {
	emit := func(p funasrInstallProgress) {
		if onProgress != nil {
			onProgress(p)
		}
	}
	pipIndex := orString(e.pipIndexURL, pipIndexURLInstall)
	if funasrSocksBlocked(pipIndex) {
		return "", errors.New(funasrSocksProxyHint)
	}
	paths := e.l.funasrPaths()
	e.stop()
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return "", err
	}
	// 清掉可能存在的半成品（models 目录保留复用）
	for _, d := range []string{filepath.Join(paths.Root, "python"), paths.VenvDir} {
		if err := e.removeDirWithRetry(d, 5); err != nil {
			return "", err
		}
	}

	// ── 阶段 1：Python 运行时 ──
	tarPath := filepath.Join(paths.Root, "python.download.tar.gz")
	expected := e.l.fetchExpectedSha256(ctx, e.pythonChecksumURLs(), pythonAssetName)
	if expected == "" {
		fmt.Println("[funasr] 未能获取官方校验和，本次安装跳过哈希校验")
	}
	err := func() error {
		defer os.Remove(tarPath)
		if err := e.downloadFirstOk(ctx, e.pythonDownloadURLs(), tarPath, expected, "runtime", onProgress); err != nil {
			return err
		}
		ectx, cancel := context.WithTimeout(ctx, orDuration(e.extractTimeout, funasrExtractTimeout))
		defer cancel()
		if err := extractTarGz(ectx, tarPath, paths.Root); err != nil {
			return fmt.Errorf("解压 Python 运行时失败：%v", err)
		}
		if !fileExists(paths.PythonExe) {
			return errors.New("解压后未找到 python.exe")
		}
		return nil
	}()
	if err != nil {
		return "", err
	}
	out, err := e.tool()(paths.PythonExe, []string{"--version"}, funasrToolOptions{})
	if err != nil {
		return "", err
	}
	pythonVersion := trimJS(out)

	// ── 阶段 2：依赖 ──
	emit(progressPhase("deps"))
	if _, err := e.tool()(paths.PythonExe, []string{"-m", "venv", paths.VenvDir}, funasrToolOptions{Timeout: funasrVenvTimeout}); err != nil {
		return "", err
	}
	var lastEmit time.Time
	args := append([]string{"-m", "pip", "install", "--no-warn-script-location"}, pipPackages...)
	args = append(args, "-i", pipIndex)
	if _, err := e.tool()(paths.VenvPython, args, funasrToolOptions{
		Timeout: orDuration(e.depsTimeout, funasrDepsTimeout),
		OnOutput: func(line string) {
			if time.Since(lastEmit) >= depsProgressThrottle {
				lastEmit = time.Now()
				emit(funasrInstallProgress{Phase: "deps", Detail: utf16Head(line, 80)})
			}
		},
	}); err != nil {
		return "", err
	}
	if !fileExists(paths.FunasrPackage) {
		return "", errors.New("依赖安装完成但未找到 funasr 包，请重试")
	}
	show, err := e.tool()(paths.VenvPython, []string{"-m", "pip", "show", "funasr"}, funasrToolOptions{})
	if err != nil {
		return "", err
	}
	funasrVersion := ""
	if m := reVersionLine.FindStringSubmatch(show); m != nil {
		funasrVersion = trimJS(m[1])
	}

	// ── 阶段 3：模型（随服务首次启动下载）──
	emit(progressPhase("models"))
	if err := e.ensure(orDuration(e.modelBootWait, funasrModelBootWait)); err != nil {
		return "", err
	}
	if err := writeFunasrState(funasrInstallState{FunasrVersion: funasrVersion, PythonVersion: pythonVersion,
		Flavor: funasrFlavorPython, InstalledAt: nowIso()}, paths); err != nil {
		return "", err
	}
	if err := e.l.writeBuiltinTranscription(false); err != nil {
		return "", err
	}
	shown := funasrVersion
	if shown == "" {
		shown = "?"
	}
	fmt.Printf("[funasr] 安装完成（funasr %s，%s），已接入语音转写路由\n", shown, pythonVersion)
	return funasrVersion, nil
}

func orString(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func (e *funasrEngine) pythonDownloadURLs() []string {
	if e.pythonURLs != nil {
		return e.pythonURLs
	}
	return getPythonDownloadURLs(e.platformOr())
}

func (e *funasrEngine) pythonChecksumURLs() []string {
	if e.checksumURLs != nil {
		return e.checksumURLs
	}
	return getPythonChecksumURLs()
}

// acquireOperation 同时只允许一个安装 / 更新 / 卸载任务。
func (e *funasrEngine) acquireOperation(msg string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.operationInFlight {
		return errors.New(msg)
	}
	e.operationInFlight = true
	return nil
}

func (e *funasrEngine) releaseOperation() { e.mu.Lock(); e.operationInFlight = false; e.mu.Unlock() }

// install 一键安装。Windows ≈ 3GB（Python + torch + 模型）。
func (e *funasrEngine) install(ctx context.Context, onProgress func(funasrInstallProgress)) (string, error) {
	if err := e.acquireOperation("已有安装 / 卸载任务进行中"); err != nil {
		return "", err
	}
	defer e.releaseOperation()
	flavor := resolveInstallFlavor(e.platformOr())
	if flavor == "" {
		if e.platformOr() == "darwin" {
			return "", errors.New("本地转写引擎的 Apple Silicon 一键安装不在当前壳内提供；请配置云端语音转写")
		}
		return "", errors.New("本地转写引擎目前仅支持 Windows（当前壳）；其他平台请配置云端语音转写")
	}
	return e.installWindowsPython(ctx, onProgress)
}

// uninstall 卸载：停服务 → 删除托管目录（含模型）→ 移除内置模型与路由。
func (e *funasrEngine) uninstall() error {
	if err := e.acquireOperation("已有安装 / 卸载任务进行中"); err != nil {
		return err
	}
	defer e.releaseOperation()
	paths := e.l.funasrPaths()
	e.stop()
	// taskkill 异步生效，等文件句柄释放
	time.Sleep(orDuration(e.stopSettle, 1500*time.Millisecond))
	if err := e.removeDirWithRetry(paths.Root, 5); err != nil {
		return err
	}
	if err := e.l.writeBuiltinTranscription(true); err != nil {
		return err
	}
	fmt.Println("[funasr] 已卸载本地转写引擎")
	return nil
}
