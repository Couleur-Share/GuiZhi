package main

// 对应 services/media/funasr-service.ts（Windows Python 形态）与 funasr-proxy.ts：
// 本地转写服务进程托管——按需启动、健康检查、随应用退出停止。
//
// 设计：服务不常驻——首次转写 / 测试 / 安装时 ensure 启动；已在监听的实例（含外部手动启动的）直接采用；
// 应用退出时结束由本模块拉起的进程。GGUF 形态（macOS）的 shim 不在 Go 壳里实现，见 funasr_paths.go。

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed funasr_server.py
var funasrServerScript string

const (
	funasrSocksProxyHint = "本地转写引擎经 Python 下载依赖和模型，暂不支持 SOCKS5 代理。请在「设置 → 网络设置」把代理协议改为 HTTP（Clash 等客户端的混合端口同时支持 HTTP）后重试。"
	funasrLogTailMax     = 4096
	funasrToolTimeout    = 60 * time.Second
)

var reSocksSupportMissing = regexp.MustCompile(`(?i)Missing dependencies for SOCKS support`)

// explainFunasrProxyFailure：Python 报缺少 SOCKS 支持时换成可操作的说明，并保留原始原因供复制。
func explainFunasrProxyFailure(message string) string {
	if reSocksSupportMissing.MatchString(message) {
		return funasrSocksProxyHint + "\n原始错误：" + message
	}
	return message
}

var (
	reAnsiEscape  = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	reProgressPct = regexp.MustCompile(`\d{1,3}%\|`)
	reProgressIt  = regexp.MustCompile(`\d+(?:\.\d+)?(?:it/s|s/it)\]`)
)

func isProgressBar(line string) bool {
	return reProgressPct.MatchString(line) || reProgressIt.MatchString(line)
}

// summarizeProcessLog 把子进程日志的尾巴整理成一句能看的报错（滤掉 tqdm 进度条，取最后 4 行）。
func summarizeProcessLog(raw string, maxChars int) string {
	var lines []string
	for _, rawLine := range regexp.MustCompile(`\r?\n`).Split(raw, -1) {
		// 终端上看得见的只有最后一次回车之后那一段
		visible := rawLine
		if i := strings.LastIndex(visible, "\r"); i >= 0 {
			visible = visible[i+1:]
		}
		text := trimJS(reAnsiEscape.ReplaceAllString(visible, ""))
		if text != "" && !isProgressBar(text) {
			lines = append(lines, text)
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	tail := strings.Join(lines, " | ")
	if utf16Len(tail) > maxChars {
		return "…" + utf16Tail(tail, maxChars)
	}
	return tail
}

func trimJS(s string) string { return strings.TrimFunc(s, jsSpace) }

// utf16Tail 对应 JS 的 s.slice(-n)（按 UTF-16 码元；落在代理对中间时丢掉半个字符）。
func utf16Tail(s string, n int) string {
	units := 0
	runes := []rune(s)
	i := len(runes)
	for i > 0 {
		w := 1
		if runes[i-1] > 0xFFFF {
			w = 2
		}
		if units+w > n {
			break
		}
		units += w
		i--
	}
	return string(runes[i:])
}

// utf16Head 对应 JS 的 s.slice(0, n)。
func utf16Head(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// ── 引擎状态 ──

type funasrProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

func (p *funasrProc) hasExited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

type funasrStart struct {
	done chan struct{}
	err  error
}

type funasrEngine struct {
	l *Library

	mu                     sync.Mutex
	child                  *funasrProc
	logGen                 int
	logTail                string
	lastActivityAt         time.Time
	starting               *funasrStart
	tail                   chan struct{} // 转写串行链的队尾；关闭表示该任务已结束
	pendingTranscriptions  int
	serviceStarts          int
	maintenanceInFlight    bool
	operationInFlight      bool
	installOrUpdateRunning bool

	// 以下为可调参数/测试接缝，零值取默认
	port          int
	bootTimeout   time.Duration
	healthPoll    time.Duration
	stopSettle    time.Duration
	waitStep      time.Duration
	runTool       funasrToolRunner
	diskSpace     diskSpaceFunc
	lookup        lookupFunc
	httpClient    *http.Client
	platform      string
	arch          string
	pypiURL       string
	pipIndexURL   string
	depsTimeout   time.Duration
	modelBootWait time.Duration

	removeRetryDelay time.Duration
	extractTimeout   time.Duration
	pythonURLs       []string
	checksumURLs     []string
}

func (l *Library) funasr() *funasrEngine {
	l.funasrOnce.Do(func() { l.funasrEng = &funasrEngine{l: l} })
	return l.funasrEng
}

func (e *funasrEngine) portNum() int {
	if e.port != 0 {
		return e.port
	}
	return funasrPortDefault
}

func (e *funasrEngine) baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d/v1", e.portNum()) }

func (e *funasrEngine) bootTimeoutOr(d time.Duration) time.Duration {
	if e.bootTimeout != 0 {
		return e.bootTimeout
	}
	return d
}

func orDuration(v, def time.Duration) time.Duration {
	if v != 0 {
		return v
	}
	return def
}

func (e *funasrEngine) platformOr() string {
	if e.platform != "" {
		return e.platform
	}
	return nodePlatform()
}

func (e *funasrEngine) archOr() string {
	if e.arch != "" {
		return e.arch
	}
	return nodeArch()
}

func nodeArch() string {
	switch goarch := runtime.GOARCH; goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return goarch
	}
}

// ── 日志汇集 ──

// funasrLogSink 单条流的日志汇入口。每条流一个解码器：UTF-8 多字节字符会被分片从中间切开，
// 逐块直接转字符串得到的是替换符。
type funasrLogSink struct {
	e       *funasrEngine
	gen     int
	pending []byte
}

func (s *funasrLogSink) write(chunk []byte) {
	buf := append(s.pending, chunk...)
	cut := len(buf)
	// 末尾若是不完整的 UTF-8 序列，留到下一块
	for i := 1; i <= utf8.UTFMax-1 && i <= len(buf); i++ {
		if utf8.RuneStart(buf[len(buf)-i]) {
			if !utf8.FullRune(buf[len(buf)-i:]) {
				cut = len(buf) - i
			}
			break
		}
	}
	text := string(buf[:cut])
	s.pending = append([]byte(nil), buf[cut:]...)
	e := s.e
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.gen != e.logGen { // 已摘掉的旧进程不再写日志：否则它的进度条会冲掉新进程的失败原因
		return
	}
	if strings.Contains(text, "[guizhi-asr] tick") {
		e.lastActivityAt = time.Now()
	}
	e.logTail = utf16Tail(e.logTail+text, funasrLogTailMax)
}

// detachLoggingLocked 摘掉上一个进程的日志汇集。
func (e *funasrEngine) detachLoggingLocked() { e.logGen++ }

// transcriptionActivityAt 托管服务最后一次心跳的时刻（零值表示本次进程还没报过）。
func (e *funasrEngine) transcriptionActivityAt() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastActivityAt
}

// ── 探测 ──

func (e *funasrEngine) client() *http.Client {
	if e.httpClient != nil {
		return e.httpClient
	}
	// 本机回环地址不走任何代理
	return &http.Client{Transport: &http.Transport{Proxy: nil}}
}

// probeHealth：/v1/models 应答即视为可用。
func (e *funasrEngine) probeHealth(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL()+"/models", nil)
	if err != nil {
		return false
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode <= 299
}

// isPortListening：端口上是否已有实例在监听。与健康探测不是一回事：转写期间服务脚本的
// generate() 是阻塞调用，/v1/models 一概不应答，但进程活得好好的、端口也占着。
func (e *funasrEngine) isPortListening(timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(e.portNum())), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ── 启动 / 停止 ──

func ensureFunasrServerScript(scriptPath string) error {
	if b, err := os.ReadFile(scriptPath); err == nil && string(b) == funasrServerScript {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(scriptPath, []byte(funasrServerScript), 0o644)
}

func (e *funasrEngine) startPythonAndWait(bootTimeout time.Duration) error {
	paths := e.l.funasrPaths()
	// 脚本内容随应用版本走：升级后无需重装引擎，这里覆盖到最新
	if err := ensureFunasrServerScript(paths.ServerScript); err != nil {
		return err
	}
	cmd := exec.Command(paths.VenvPython, paths.ServerScript, "--device", "cpu", "--model", funasrModelName,
		"--host", "127.0.0.1", "--port", strconv.Itoa(e.portNum()))
	hideChildWindow(cmd)
	// 不指定的话 Windows 上 Python 按 ANSI 代码页写管道（简中是 cp936），这边按 UTF-8 解会整段乱码
	cmd.Env = append(os.Environ(), "MODELSCOPE_CACHE="+paths.ModelsDir, "PYTHONIOENCODING=utf-8")
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	proc := &funasrProc{cmd: cmd, exited: make(chan struct{})}
	e.mu.Lock()
	e.logTail = ""
	e.detachLoggingLocked()
	gen := e.logGen
	e.child = proc
	e.mu.Unlock()

	if err := cmd.Start(); err != nil {
		close(proc.exited) // 对应 'error' 事件：进程没起来
	} else {
		var wg sync.WaitGroup
		for _, r := range []io.Reader{stdout, stderr} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sink := &funasrLogSink{e: e, gen: gen}
				br := bufio.NewReaderSize(r, 32*1024)
				buf := make([]byte, 16*1024)
				for {
					n, err := br.Read(buf)
					if n > 0 {
						sink.write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}()
		}
		go func() { wg.Wait(); _ = cmd.Wait(); close(proc.exited) }()
	}

	deadline := time.Now().Add(bootTimeout)
	for time.Now().Before(deadline) {
		if proc.hasExited() {
			e.mu.Lock()
			reason := summarizeProcessLog(e.logTail, 300)
			e.detachLoggingLocked()
			e.child = nil
			e.mu.Unlock()
			if reason == "" {
				reason = "进程异常退出（无可用日志）"
			}
			return fmt.Errorf("本地转写服务启动失败：%s", explainFunasrProxyFailure(reason))
		}
		if e.probeHealth(2 * time.Second) {
			fmt.Printf("[funasr] 服务已就绪（127.0.0.1:%d）\n", e.portNum())
			return nil
		}
		time.Sleep(orDuration(e.healthPoll, 2*time.Second))
	}
	e.stop()
	return errors.New("本地转写服务启动超时")
}

// startOrAdopt：健康探测失败之后的分流——端口被占着就认下这个实例，否则才拉新的。
func (e *funasrEngine) startOrAdopt(bootTimeout time.Duration) error {
	if e.isPortListening(1500 * time.Millisecond) {
		fmt.Println("[funasr] 端口已被实例占用（正忙），本次排队复用")
		return nil
	}
	paths := e.l.funasrPaths()
	if !isFunasrInstalled(paths) {
		return errors.New("本地转写引擎未安装")
	}
	switch resolveFunasrEngineFlavor(paths) {
	case funasrFlavorPython:
		return e.startPythonAndWait(bootTimeout)
	case funasrFlavorGguf:
		return errors.New("本地转写引擎为 macOS GGUF 形态，当前壳不提供拉起支持；请改用 Windows 引擎或配置云端语音转写")
	}
	return errors.New("本地转写引擎未安装")
}

// ensure 确保托管服务可用：已就绪或正忙则直接采用；否则拉起并等待健康。
func (e *funasrEngine) ensure(bootTimeout time.Duration) error {
	e.mu.Lock()
	if e.maintenanceInFlight {
		e.mu.Unlock()
		return errors.New("本地转写引擎正在更新，请稍后重试")
	}
	e.serviceStarts++
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.serviceStarts--; e.mu.Unlock() }()
	return e.ensureInternal(bootTimeout)
}

func (e *funasrEngine) ensureInternal(bootTimeout time.Duration) error {
	if e.probeHealth(3 * time.Second) {
		return nil
	}
	// 判断与登记在同一把锁内：并发调用不会各拉一个进程
	e.mu.Lock()
	if s := e.starting; s != nil {
		e.mu.Unlock()
		<-s.done
		return s.err
	}
	s := &funasrStart{done: make(chan struct{})}
	e.starting = s
	e.mu.Unlock()
	s.err = e.startOrAdopt(bootTimeout)
	e.mu.Lock()
	e.starting = nil
	e.mu.Unlock()
	close(s.done)
	return s.err
}

// runMaintenance 更新与转写 / 启动互斥；维护中的健康验收通过专用回调启动。
func (e *funasrEngine) runMaintenance(task func(startService func() error) error) error {
	e.mu.Lock()
	if e.maintenanceInFlight || e.pendingTranscriptions > 0 || e.serviceStarts > 0 {
		e.mu.Unlock()
		return errors.New("本地引擎正在转写或启动，请完成后再更新")
	}
	e.maintenanceInFlight = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.maintenanceInFlight = false; e.mu.Unlock() }()
	return task(func() error { return e.ensureInternal(e.bootTimeoutOr(2 * time.Minute)) })
}

// runExclusiveLocalTranscription 串行化本地引擎的转写请求（FIFO）。
//
// 服务端本来就一次只做一条，但客户端不排队的话，后到的请求会带着自己的超时预算干等前一条做完，
// 等出来的失败看着像「转写超时」。锁在这里，超时才从真正开始干活时起算。
func runExclusiveLocalTranscription[T any](e *funasrEngine, task func() (T, error)) (T, error) {
	e.mu.Lock()
	if e.maintenanceInFlight {
		e.mu.Unlock()
		var zero T
		return zero, errors.New("本地转写引擎正在更新，请稍后重试")
	}
	e.pendingTranscriptions++
	prev := e.tail
	mine := make(chan struct{})
	e.tail = mine
	e.mu.Unlock()
	if prev != nil {
		<-prev // 前一条的成败不影响后一条排队，链条只用来定序
	}
	defer func() {
		close(mine)
		e.mu.Lock()
		e.pendingTranscriptions--
		e.mu.Unlock()
	}()
	return task()
}

// stop 停止由本模块拉起的服务进程（Windows 下杀进程树，避免遗留 python 子进程）。
func (e *funasrEngine) stop() {
	e.mu.Lock()
	proc := e.child
	e.detachLoggingLocked()
	e.child = nil
	e.mu.Unlock()
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	pid := proc.cmd.Process.Pid
	if isWindows() {
		k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
		hideChildWindow(k)
		if k.Start() == nil {
			go func() { _ = k.Wait() }()
		} else {
			_ = proc.cmd.Process.Kill()
		}
	} else {
		_ = proc.cmd.Process.Kill()
	}
	fmt.Printf("[funasr] 已停止服务进程（pid=%d）\n", pid)
}

// ensureLocalTranscriptionService 转写前置钩子：目标是托管服务地址且引擎已安装时，确保服务在运行。
// 指向其他地址（云端 / 用户自管服务）时不做任何事。
func (e *funasrEngine) ensureLocalTranscriptionService(apiURL string) error {
	if !isManagedFunasrURL(apiURL) || !isFunasrInstalled(e.l.funasrPaths()) {
		return nil
	}
	return e.ensure(e.bootTimeoutOr(2 * time.Minute))
}

// supportsLocalDiarization 说话人分离只绑 Windows Python 引擎（cam++）。
func (e *funasrEngine) supportsLocalDiarization(apiURL string) bool {
	return isManagedFunasrURL(apiURL) && isFunasrPythonInstalled(e.l.funasrPaths())
}
