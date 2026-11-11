package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFunasrServiceLifecycle(t *testing.T) {
	fx := newFunasrFixture(t)
	fx.installLayout("1.3.29")
	e := fx.e

	// 并发 ensure 只拉起一个进程
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.ensure(15 * time.Second); err != nil {
				t.Errorf("ensure: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := fx.serverStarts(); n != 1 {
		t.Fatalf("并发启动应只有一个进程：%d", n)
	}
	if !e.probeHealth(time.Second) {
		t.Fatal("服务应健康")
	}
	if b, _ := os.ReadFile(fx.paths.ServerScript); string(b) != funasrServerScript {
		t.Error("服务脚本应落盘为内嵌版本")
	}
	if b, _ := os.ReadFile(filepath.Join(fx.paths.Root, "server-starts.log")); !strings.Contains(string(b), "env="+fx.paths.ModelsDir) {
		t.Errorf("应把 MODELSCOPE_CACHE 指向模型目录：%s", b)
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.transcriptionActivityAt().IsZero() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.transcriptionActivityAt().IsZero() {
		t.Error("服务启动时的心跳应被记录")
	}
	if err := e.ensure(time.Second); err != nil || fx.serverStarts() != 1 {
		t.Errorf("已在运行时不应再拉进程：%v / %d", err, fx.serverStarts())
	}
	e.stop()
	fx.waitStopped()
	if err := e.ensure(15 * time.Second); err != nil || fx.serverStarts() != 2 {
		t.Errorf("停止后应能重新拉起：%v / %d", err, fx.serverStarts())
	}

	// 转写前置钩子与说话人分离的判定
	managed := "http://127.0.0.1:8620/v1"
	if !e.supportsLocalDiarization(managed) || e.supportsLocalDiarization("https://api.example.com/v1") {
		t.Error("说话人分离只对托管地址且 Python 引擎已装成立")
	}
	if err := e.ensureLocalTranscriptionService("https://api.example.com/v1"); err != nil {
		t.Errorf("非托管地址不应有动作：%v", err)
	}
	if err := e.ensureLocalTranscriptionService(managed); err != nil {
		t.Errorf("托管地址应确保服务在运行：%v", err)
	}
}

func TestFunasrServiceStartFailures(t *testing.T) {
	t.Run("未安装", func(t *testing.T) {
		fx := newFunasrFixture(t)
		if err := fx.e.ensure(time.Second); err == nil || err.Error() != "本地转写引擎未安装" {
			t.Errorf("%v", err)
		}
		if fx.e.supportsLocalDiarization("http://127.0.0.1:8620/v1") {
			t.Error("未安装不支持分离")
		}
		if err := fx.e.ensureLocalTranscriptionService("http://127.0.0.1:8620/v1"); err != nil {
			t.Errorf("未安装时前置钩子不应报错：%v", err)
		}
	})
	t.Run("启动即崩溃", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		t.Setenv("FAKE_FAIL", "boot-crash")
		err := fx.e.ensure(10 * time.Second)
		want := "本地转写服务启动失败：Traceback (most recent call last): | RuntimeError: 模型加载失败"
		if err == nil || err.Error() != want {
			t.Errorf("崩溃原因应滤掉进度条：%v", err)
		}
		if fx.e.child != nil {
			t.Error("崩溃后不应保留子进程引用")
		}
	})
	t.Run("缺少 SOCKS 支持", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		t.Setenv("FAKE_FAIL", "boot-crash")
		t.Setenv("FAKE_BOOT_MSG", "Missing dependencies for SOCKS support")
		err := fx.e.ensure(10 * time.Second)
		if err == nil || !strings.Contains(err.Error(), funasrSocksProxyHint) || !strings.Contains(err.Error(), "原始错误：") {
			t.Errorf("%v", err)
		}
	})
	t.Run("启动超时", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		t.Setenv("FAKE_FAIL", "boot-hang")
		start := time.Now()
		err := fx.e.ensure(1200 * time.Millisecond)
		if err == nil || err.Error() != "本地转写服务启动超时" || time.Since(start) > 15*time.Second {
			t.Errorf("%v（%v）", err, time.Since(start))
		}
		if fx.e.child != nil {
			t.Error("超时后应已停掉子进程")
		}
	})
	t.Run("端口被占用时认下实例", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fx.e.port))
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() { // 接受连接就关：健康探测失败，但端口确实占着
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		if err := fx.e.ensure(5 * time.Second); err != nil {
			t.Errorf("正忙的实例应被复用：%v", err)
		}
		if fx.serverStarts() != 0 {
			t.Error("不应再拉新进程")
		}
	})
	t.Run("GGUF 形态不拉起", func(t *testing.T) {
		fx := newFunasrFixture(t)
		p := fx.paths
		for _, f := range []string{p.SensevoiceCli, p.SensevoiceGGU, p.VadGguf} {
			os.MkdirAll(filepath.Dir(f), 0o755)
			os.WriteFile(f, []byte("x"), 0o644)
		}
		if st := fx.e.status(); !st.Installed || st.InstallFlavor != funasrFlavorGguf || st.UpdateSupported {
			t.Errorf("GGUF 应识别为已装但不支持独立更新：%+v", st)
		}
		if err := fx.e.ensure(time.Second); err == nil || !strings.Contains(err.Error(), "GGUF") {
			t.Errorf("%v", err)
		}
	})
	t.Run("维护互斥", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		err := fx.e.runMaintenance(func(func() error) error {
			if err := fx.e.ensure(time.Second); err == nil || err.Error() != "本地转写引擎正在更新，请稍后重试" {
				t.Errorf("维护中不应启动服务：%v", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		done, release := make(chan struct{}), make(chan struct{})
		go runExclusiveLocalTranscription(fx.e, func() (int, error) { close(done); <-release; return 0, nil })
		<-done
		if err := fx.e.runMaintenance(func(func() error) error { return nil }); err == nil || err.Error() != "本地引擎正在转写或启动，请完成后再更新" {
			t.Errorf("转写中不应进入维护：%v", err)
		}
		close(release)
	})
}

func TestFunasrStatus(t *testing.T) {
	fx := newFunasrFixture(t)
	st := fx.e.status()
	if st.Installed || st.Running || st.Port != fx.e.port || st.Dir != fx.paths.Root || !st.InstallSupported || !st.UpdateSupported || st.InstallFlavor != "python" || st.Version != "" {
		t.Errorf("未安装：%+v", st)
	}
	fx.installLayout("1.3.29")
	if err := fx.e.ensure(15 * time.Second); err != nil {
		t.Fatal(err)
	}
	st = fx.e.status()
	if !st.Installed || !st.Running || st.Version != "1.3.29" || st.InstallFlavor != "python" {
		t.Errorf("已安装且运行：%+v", st)
	}
	raw, _ := json.Marshal(st)
	if !strings.Contains(string(raw), `"updateSupported":true`) || strings.Contains(string(raw), `"installFlavor":""`) {
		t.Errorf("序列化形态：%s", raw)
	}
	fx.e.platform = "linux"
	if st := fx.e.status(); st.InstallSupported || st.UpdateSupported || st.InstallFlavor != "python" {
		// 已装的 Python 引擎仍按已装形态报告，只是该平台不提供一键安装与更新
		t.Errorf("linux：%+v", st)
	}
	fx.e.stop()
	fx.waitStopped()
	os.RemoveAll(fx.paths.Root)
	if st := fx.e.status(); st.InstallFlavor != "" || st.InstallSupported {
		t.Errorf("linux 且未安装：%+v", st)
	}
}

func TestFunasrInstallUninstall(t *testing.T) {
	fx := newFunasrFixture(t)
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.29")
	archive := pythonTarGz(t, true)
	fx.serveInstallAssets(archive, true)
	// models 目录是下载过的模型缓存：重装必须保留
	os.MkdirAll(fx.paths.ModelsDir, 0o755)
	os.WriteFile(filepath.Join(fx.paths.ModelsDir, "keep.bin"), []byte("model"), 0o644)

	var mu sync.Mutex
	var phases []string
	sawPercent := false
	version, err := fx.e.install(context.Background(), func(p funasrInstallProgress) {
		mu.Lock()
		defer mu.Unlock()
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
		if p.Phase == "runtime" && p.Percent != nil && *p.Percent == 100 {
			sawPercent = true
		}
	})
	if err != nil || version != "1.3.29" {
		t.Fatalf("安装：%q %v", version, err)
	}
	if strings.Join(phases, ",") != "runtime,deps,models" || !sawPercent {
		t.Errorf("进度阶段：%v（100%%=%v）", phases, sawPercent)
	}
	if fileExists(filepath.Join(fx.paths.Root, "python.download.tar.gz")) {
		t.Error("下载的压缩包应被清理")
	}
	if b, _ := os.ReadFile(filepath.Join(fx.paths.ModelsDir, "keep.bin")); string(b) != "model" {
		t.Error("models 目录应保留")
	}
	st := readFunasrState(fx.paths)
	if st == nil || st.FunasrVersion != "1.3.29" || st.PythonVersion != "Python 3.12.13" || st.Flavor != "python" || st.InstalledAt == "" {
		t.Errorf("state.json：%+v", st)
	}
	cfg, err := fx.l.readAIConfig()
	if err != nil || len(cfg.Providers) != 1 || len(cfg.Models) != 1 || cfg.Routes.vals["audioText"] != funasrModelID {
		t.Errorf("AI 配置应接入语音转写路由：%+v %v", cfg, err)
	}
	if s := fx.e.status(); !s.Installed || !s.Running || s.Version != "1.3.29" {
		t.Errorf("安装后状态：%+v", s)
	}
	if fx.serverStarts() != 1 {
		t.Errorf("阶段 3 应只拉起一次服务：%d", fx.serverStarts())
	}
	// 任务互斥
	fx.e.acquireOperation("x")
	if _, err := fx.e.install(context.Background(), nil); err == nil || err.Error() != "已有安装 / 卸载任务进行中" {
		t.Errorf("并发安装：%v", err)
	}
	if err := fx.e.uninstall(); err == nil || err.Error() != "已有安装 / 卸载任务进行中" {
		t.Errorf("安装中卸载：%v", err)
	}
	fx.e.releaseOperation()

	if err := fx.e.uninstall(); err != nil {
		t.Fatal(err)
	}
	fx.waitStopped()
	if fileExists(fx.paths.Root) {
		t.Error("卸载应删掉托管目录（含模型）")
	}
	cfg, _ = fx.l.readAIConfig()
	if len(cfg.Providers) != 0 || len(cfg.Models) != 0 || cfg.Routes.vals["audioText"] != nil {
		t.Errorf("卸载应移除内置条目与路由：%+v", cfg)
	}
}

func TestFunasrInstallFailures(t *testing.T) {
	t.Run("校验和不匹配", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.serveInstallAssets(pythonTarGz(t, true), false)
		_, err := fx.e.install(context.Background(), nil)
		if err == nil || !strings.HasPrefix(err.Error(), "全部下载源失败——dl.test: 校验和不匹配（期望 aaaaaaaaaaaa…，实际 ") {
			t.Errorf("%v", err)
		}
		if fileExists(filepath.Join(fx.paths.Root, "python.download.tar.gz")) || fileExists(fx.paths.PythonExe) {
			t.Error("失败后不应留下半成品")
		}
	})
	t.Run("镜像回退且取不到校验和时跳过校验", func(t *testing.T) {
		fx := newFunasrFixture(t)
		t.Setenv("FAKE_FUNASR_VERSION", "1.3.29")
		fx.e.pythonURLs = []string{"https://dl.test/bad", "https://dl.test/python.tar.gz"}
		fx.e.checksumURLs = []string{"https://dl.test/missing"}
		fx.mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) })
		fx.mux.HandleFunc("/python.tar.gz", serveBytes(pythonTarGz(t, true)))
		if v, err := fx.e.install(context.Background(), nil); err != nil || v != "1.3.29" {
			t.Fatalf("%q %v", v, err)
		}
		if fx.hit("dl.test/bad") != 1 {
			t.Error("第一个源应被尝试")
		}
	})
	for name, c := range map[string]struct{ fail, want string }{
		"pip 失败":      {"install-pip", "退出码 1: ERROR: Could not find a version that satisfies the requirement torch"},
		"装完没有 funasr": {"install-nopkg", "依赖安装完成但未找到 funasr 包，请重试"},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFunasrFixture(t)
			t.Setenv("FAKE_FAIL", c.fail)
			fx.serveInstallAssets(pythonTarGz(t, true), true)
			_, err := fx.e.install(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%v", err)
			}
			if _, err := fx.l.readAIConfig(); err != nil || fileExists(fx.paths.StateFile) {
				t.Error("失败时不应写 state.json")
			}
		})
	}
	t.Run("压缩包里没有 python.exe", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.serveInstallAssets(pythonTarGz(t, false), true)
		if _, err := fx.e.install(context.Background(), nil); err == nil || err.Error() != "解压后未找到 python.exe" {
			t.Errorf("%v", err)
		}
	})
	t.Run("SOCKS5 代理先于一切动作被拒绝", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.serveInstallAssets(pythonTarGz(t, true), true)
		keep := filepath.Join(fx.paths.Root, "python", "keep.txt")
		os.MkdirAll(filepath.Dir(keep), 0o755)
		os.WriteFile(keep, []byte("old"), 0o644)
		activeNetProxy.Apply(map[string]any{"mode": "manual", "protocol": "socks5", "host": "127.0.0.1", "port": float64(1080)})
		t.Cleanup(func() { activeNetProxy.Apply(nil) })
		if _, err := fx.e.install(context.Background(), nil); err == nil || err.Error() != funasrSocksProxyHint {
			t.Errorf("%v", err)
		}
		if !fileExists(keep) || fx.hit("dl.test/python.tar.gz") != 0 {
			t.Error("被拒绝时不应清理旧环境或下载")
		}
	})
	t.Run("不支持的平台", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.e.platform = "linux"
		if _, err := fx.e.install(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
			t.Errorf("%v", err)
		}
		fx.e.platform = "darwin"
		if _, err := fx.e.install(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
			t.Errorf("%v", err)
		}
	})
}

func TestFunasrCheckUpdate(t *testing.T) {
	fx := newFunasrFixture(t)
	ctx := context.Background()
	if _, err := fx.e.checkUpdate(ctx); err == nil || !strings.Contains(err.Error(), "当前引擎不支持独立更新") {
		t.Errorf("未安装：%v", err)
	}
	fx.installLayout("1.3.29")
	fx.setPyPI("1.3.30")
	c, err := fx.e.checkUpdate(ctx)
	if err != nil || *c.Current != "1.3.29" || *c.Latest != "1.3.30" || !c.UpdateAvailable {
		t.Errorf("有更新：%+v %v", c, err)
	}
	fx.setPyPI("1.3.29")
	if c, err := fx.e.checkUpdate(ctx); err != nil || c.UpdateAvailable {
		t.Errorf("已是最新：%+v %v", c, err)
	}
	for name, body := range map[string]string{
		"撤回的版本":   pypiBody("1.3.30", true, false),
		"文件全被撤回":  pypiBody("1.3.30", false, true, true),
		"没有文件":    pypiBody("1.3.30", false),
		"预发布":     pypiBody("2.0rc1", false, false),
		"缺 info":  `{"urls":[{"yanked":false}]}`,
		"文件缺撤回标记": `{"info":{"version":"1.3.30"},"urls":[{}]}`,
	} {
		fx.pypi = body
		if _, err := fx.e.checkUpdate(ctx); err == nil || err.Error() != "PyPI 未返回可用的 FunASR 稳定版本" {
			t.Errorf("%s：%v", name, err)
		}
	}
	fx.pypi = `not json`
	if _, err := fx.e.checkUpdate(ctx); err == nil {
		t.Error("坏 JSON 应报错")
	}
	fx.pypiStatus = 503
	if _, err := fx.e.checkUpdate(ctx); err == nil || err.Error() != "PyPI 检查更新失败：HTTP 503" {
		t.Errorf("%v", err)
	}
	fx.pypiStatus = 0
	fx.e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}
	if _, err := fx.e.checkUpdate(ctx); err == nil {
		t.Error("域名解析到内网地址应被拒绝")
	}
	fx.e.lookup = nil
	writeFunasrState(funasrInstallState{InstalledAt: "x", Flavor: "python"}, fx.paths)
	if _, err := fx.e.checkUpdate(ctx); err == nil || err.Error() != "无法读取已安装版本，请重新安装引擎" {
		t.Errorf("缺少版本：%v", err)
	}
	fx.e.platform = "linux"
	if _, err := fx.e.checkUpdate(ctx); err == nil || !strings.Contains(err.Error(), "不支持独立更新") {
		t.Errorf("非 Windows：%v", err)
	}
}

func (fx *funasrFixture) backupDirs() []string {
	es, _ := os.ReadDir(fx.paths.Root)
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "update-backup-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func (fx *funasrFixture) readyEngine(current, remote string) {
	fx.t.Helper()
	fx.installLayout(current)
	fx.setPyPI(remote)
	if err := fx.e.ensure(15 * time.Second); err != nil {
		fx.t.Fatal(err)
	}
}

func TestFunasrUpdateSuccess(t *testing.T) {
	fx := newFunasrFixture(t)
	fx.readyEngine("1.3.29", "1.3.30")
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.30")
	var phases []string
	v, warning, err := fx.e.update(context.Background(), "1.3.30", func(p funasrInstallProgress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	})
	if err != nil || v != "1.3.30" || warning != "" {
		t.Fatalf("更新：%q %q %v", v, warning, err)
	}
	if strings.Join(phases, ",") != "prepare,backup,deps,verify" {
		t.Errorf("阶段：%v", phases)
	}
	st := readFunasrState(fx.paths)
	if st.FunasrVersion != "1.3.30" || st.PythonVersion != "Python 3.12.13" || st.InstalledAt != "2026-01-02T03:04:05.000Z" {
		t.Errorf("state.json 只应改版本：%+v", st)
	}
	if len(fx.backupDirs()) != 0 {
		t.Errorf("验收通过后备份应被清掉：%v", fx.backupDirs())
	}
	if !fx.e.probeHealth(2*time.Second) || fx.serverStarts() != 2 {
		t.Errorf("更新后服务应重新就绪（启动 %d 次）", fx.serverStarts())
	}
	if !fileExists(filepath.Join(fx.paths.VenvDir, "keep.txt")) {
		t.Error("原环境内容应保留")
	}
}

func TestFunasrUpdateRollback(t *testing.T) {
	for _, fail := range []string{"update-pip", "update-check"} {
		t.Run(fail, func(t *testing.T) {
			fx := newFunasrFixture(t)
			fx.readyEngine("1.3.29", "1.3.30")
			t.Setenv("FAKE_FUNASR_VERSION", "1.3.30")
			t.Setenv("FAKE_FAIL", fail)
			var phases []string
			_, _, err := fx.e.update(context.Background(), "1.3.30", func(p funasrInstallProgress) {
				if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
					phases = append(phases, p.Phase)
				}
			})
			if err == nil || !strings.HasPrefix(err.Error(), "更新失败，已恢复原版本：Error: ") {
				t.Fatalf("应报告已恢复：%v", err)
			}
			if phases[len(phases)-1] != "rollback" {
				t.Errorf("最后阶段应为 rollback：%v", phases)
			}
			if fileExists(filepath.Join(fx.paths.VenvDir, "Lib", "site-packages", "funasr", "MODIFIED")) {
				t.Error("失败的改动应被还原")
			}
			if !fileExists(filepath.Join(fx.paths.VenvDir, "keep.txt")) || !fileExists(fx.paths.FunasrPackage) {
				t.Error("原环境应完整恢复")
			}
			if st := readFunasrState(fx.paths); st.FunasrVersion != "1.3.29" {
				t.Errorf("版本应回到旧版：%+v", st)
			}
			if len(fx.backupDirs()) != 0 {
				t.Errorf("恢复成功后备份应被清掉：%v", fx.backupDirs())
			}
			t.Setenv("FAKE_FAIL", "")
			if !fx.e.probeHealth(2 * time.Second) {
				t.Error("恢复后服务应重新可用")
			}
		})
	}
}

func TestFunasrUpdateRollbackBlocked(t *testing.T) {
	fx := newFunasrFixture(t)
	fx.readyEngine("1.3.29", "1.3.30")
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.30")
	t.Setenv("FAKE_FAIL", "update-pip-blockrollback")
	_, _, err := fx.e.update(context.Background(), "1.3.30", nil)
	if err == nil || !strings.Contains(err.Error(), "；恢复失败：") || !strings.Contains(err.Error(), "失败环境目录已存在，拒绝覆盖") || !strings.Contains(err.Error(), "备份保留在 ") {
		t.Fatalf("%v", err)
	}
	dirs := fx.backupDirs()
	if len(dirs) != 1 {
		t.Fatalf("备份必须保留：%v", dirs)
	}
	m, merr := readBackupManifest(fx.paths.Root, filepath.Join(fx.paths.Root, dirs[0]), dirs[0])
	if merr != nil || m.Phase != backupRecoveryRequired {
		t.Errorf("备份应标记为需要恢复：%+v %v", m, merr)
	}
	warnings, _ := cleanupFunasrBackups(fx.paths.Root)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "保留使用中或可能需要恢复的备份") {
		t.Errorf("清理不应动它：%v", warnings)
	}
}

func TestFunasrUpdatePrechecks(t *testing.T) {
	ctx := context.Background()
	t.Run("磁盘空间不足不影响原引擎", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.readyEngine("1.3.29", "1.3.30")
		fx.e.diskSpace = func(string) (int64, int64, error) { return 1 << 20, 4096, nil }
		_, _, err := fx.e.update(ctx, "1.3.30", nil)
		if err == nil || !strings.HasPrefix(err.Error(), "磁盘空间不足，更新尚未开始：") {
			t.Fatalf("%v", err)
		}
		if !fx.e.probeHealth(2*time.Second) || len(fx.backupDirs()) != 0 || fx.serverStarts() != 1 {
			t.Error("服务不应被停掉，也不应产生备份")
		}
	})
	t.Run("磁盘查询失败", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.readyEngine("1.3.29", "1.3.30")
		fx.e.diskSpace = func(string) (int64, int64, error) { return 0, 0, fmt.Errorf("boom") }
		if _, _, err := fx.e.update(ctx, "1.3.30", nil); err == nil || !strings.HasPrefix(err.Error(), "无法检查引擎磁盘空间，更新尚未开始：") {
			t.Errorf("%v", err)
		}
	})
	t.Run("版本已变化 / 已是最新 / 非法版本", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.readyEngine("1.3.29", "1.3.30")
		if _, _, err := fx.e.update(ctx, "1.3.31", nil); err == nil || err.Error() != "可用版本已变化，请重新检查更新" {
			t.Errorf("%v", err)
		}
		for _, bad := range []string{"", "1.3.30; calc", "latest", "1.3.30rc1"} {
			if _, _, err := fx.e.update(ctx, bad, nil); err == nil || err != errInvalidFunasrVersion {
				t.Errorf("%q：%v", bad, err)
			}
		}
		fx.setPyPI("1.3.29")
		if v, w, err := fx.e.update(ctx, "1.3.29", nil); err != nil || v != "1.3.29" || w != "" {
			t.Errorf("已是最新应直接返回当前版本：%q %q %v", v, w, err)
		}
		if fx.serverStarts() != 1 || len(fx.backupDirs()) != 0 {
			t.Error("已是最新时不应动服务或备份")
		}
	})
	t.Run("任务互斥与转写互斥", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.readyEngine("1.3.29", "1.3.30")
		fx.e.acquireOperation("x")
		if _, _, err := fx.e.update(ctx, "1.3.30", nil); err == nil || err.Error() != "已有安装 / 更新 / 卸载任务进行中" {
			t.Errorf("%v", err)
		}
		fx.e.releaseOperation()
		done, release := make(chan struct{}), make(chan struct{})
		go runExclusiveLocalTranscription(fx.e, func() (int, error) { close(done); <-release; return 0, nil })
		<-done
		_, _, err := fx.e.update(ctx, "1.3.30", nil)
		close(release)
		if err == nil || err.Error() != "本地引擎正在转写或启动，请完成后再更新" {
			t.Errorf("%v", err)
		}
		if !fx.e.probeHealth(2 * time.Second) {
			t.Error("被拒绝时不应停掉服务")
		}
	})
	t.Run("外部实例占着端口时拒绝覆盖依赖", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.installLayout("1.3.29")
		fx.setPyPI("1.3.30")
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fx.e.port))
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		_, _, err = fx.e.update(ctx, "1.3.30", nil)
		if err == nil || err.Error() != "本地转写服务仍在运行，请关闭外部转写服务后重试" {
			t.Errorf("%v", err)
		}
		if len(fx.backupDirs()) != 0 || !fileExists(filepath.Join(fx.paths.VenvDir, "keep.txt")) {
			t.Error("依赖没动过，备份应被清掉")
		}
	})
	t.Run("SOCKS5 代理先于一切动作被拒绝", func(t *testing.T) {
		fx := newFunasrFixture(t)
		fx.readyEngine("1.3.29", "1.3.30")
		activeNetProxy.Apply(map[string]any{"mode": "manual", "protocol": "socks5", "host": "127.0.0.1", "port": float64(1080)})
		t.Cleanup(func() { activeNetProxy.Apply(nil) })
		if _, _, err := fx.e.update(ctx, "1.3.30", nil); err == nil || err.Error() != funasrSocksHint() {
			t.Errorf("%v", err)
		}
		if fx.hit("pypi.test/pypi/funasr/json") != 0 || !fx.e.probeHealth(2*time.Second) {
			t.Error("被拒绝时不应访问 PyPI 或停服务")
		}
	})
}

func funasrSocksHint() string { return funasrSocksProxyHint }

func TestFunasrUpdateRepairsBrokenPip(t *testing.T) {
	fx := newFunasrFixture(t)
	fx.readyEngine("1.3.29", "1.3.30")
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.30")
	t.Setenv("FAKE_PIP_BROKEN", "1")
	var repaired bool
	_, _, err := fx.e.update(context.Background(), "1.3.30", func(p funasrInstallProgress) {
		if p.Detail == "正在离线修复更新工具 pip" {
			repaired = true
		}
	})
	if err != nil || !repaired {
		t.Fatalf("pip 损坏时应先离线修复再更新：repaired=%v err=%v", repaired, err)
	}
}

func TestEnsureFunasrPipRepairFails(t *testing.T) {
	calls := 0
	err := ensureFunasrPip("python", func(string, []string, funasrToolOptions) (string, error) {
		calls++
		return "", fmt.Errorf("broken")
	}, nil)
	if err == nil || err.Error() != "本地引擎 pip 离线修复失败：Error: broken" || calls != 2 {
		t.Errorf("%v（调用 %d 次）", err, calls)
	}
}

func TestFunasrBackupLifecycle(t *testing.T) {
	root := t.TempDir()
	state := funasrInstallState{FunasrVersion: "1.3.29", InstalledAt: "2026-01-01T00:00:00.000Z"}
	b, err := createFunasrBackup(root, state, "1.3.30")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(b.Directory, "env"), 0o755)
	os.WriteFile(filepath.Join(b.Directory, "env", "f.txt"), []byte("x"), 0o644)
	// 非 disposable 的备份不会被清理
	if ws, _ := cleanupFunasrBackups(root); len(ws) != 1 || !strings.Contains(ws[0], "保留使用中或可能需要恢复的备份") {
		t.Errorf("%v", ws)
	}
	if w := discardFunasrBackup(b); w != "" || fileExists(b.Directory) {
		t.Errorf("正常丢弃应删除目录：%q", w)
	}
	// 目录被换成别的节点（身份变化）：拒绝
	b2, _ := createFunasrBackup(root, state, "1.3.30")
	os.RemoveAll(b2.Directory)
	os.Mkdir(b2.Directory, 0o700)
	if err := markFunasrBackup(b2, backupReady); err == nil || !strings.Contains(err.Error(), "目录身份发生变化") {
		t.Errorf("身份变化应被拒绝：%v", err)
	}
	// 越界与非法名字
	if err := assertBackupPath(root, filepath.Join(root, "other")); err == nil {
		t.Error("名字不符应被拒绝")
	}
	if err := assertBackupPath(root, filepath.Join(filepath.Dir(root), "update-backup-abcdef")); err == nil {
		t.Error("越界应被拒绝")
	}
	// 清单内容在清理期间被改成 recovery-required：不再删除
	b3, _ := createFunasrBackup(root, state, "1.3.30")
	markFunasrBackup(b3, backupDisposable)
	snapshot := *b3
	data, _ := marshalIndent2(func() funasrBackupManifest { m := snapshot.Manifest; m.Phase = backupRecoveryRequired; return m }())
	os.WriteFile(filepath.Join(b3.Directory, funasrBackupManifestName), data, 0o644)
	if err := removeBackup(&snapshot); err == nil || err.Error() != "备份状态发生变化，保留文件" {
		t.Errorf("%v", err)
	}
	if !fileExists(b3.Directory) {
		t.Error("状态变化时不应删除")
	}
	// 目录里有用户放进来的文件：保留整个目录
	b4, _ := createFunasrBackup(root, state, "1.3.30")
	markFunasrBackup(b4, backupDisposable)
	os.WriteFile(filepath.Join(b4.Directory, "mine.txt"), []byte("x"), 0o644)
	if w := discardFunasrBackup(b4); !strings.Contains(w, "备份含有非预期内容，保留整个目录：mine.txt") || !fileExists(b4.Directory) {
		t.Errorf("%q", w)
	}
}

func TestFunasrPlainTreeAndSpace(t *testing.T) {
	if !isWindows() {
		t.Skip("硬链接与联接点判定按 Windows 口径")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	if err := assertPlainFunasrTree(dir); err != nil {
		t.Errorf("普通目录树应通过：%v", err)
	}
	if err := os.Link(filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub", "hard.txt")); err == nil {
		if err := assertPlainFunasrTree(dir); err == nil || !strings.Contains(err.Error(), "发现链接或异常文件") {
			t.Errorf("硬链接应被拒绝：%v", err)
		}
		if _, err := measureFunasrEnvironment(dir, 4096); err != nil {
			t.Errorf("体积估算不查硬链接：%v", err)
		}
	}
	// 真实卷查询：可用空间为正，分配单位为 2 的幂
	avail, block, err := systemDiskSpace(dir)
	if err != nil || avail <= 0 || block <= 0 || block&(block-1) != 0 {
		t.Errorf("systemDiskSpace：%d %d %v", avail, block, err)
	}
	// 预算：两份环境（至少 1 GiB）+ 512 MiB 余量
	enough := func(string) (int64, int64, error) { return 1 << 40, 4096, nil }
	b, err := planFunasrUpdateSpace(enough, dir, dir)
	if err != nil || b.WorkBytes != funasrGiB+funasrReserveBytes {
		t.Errorf("预算：%+v %v", b, err)
	}
	tight := func(string) (int64, int64, error) { return b.BackupBytes + b.WorkBytes - 1, 4096, nil }
	if _, err := planFunasrUpdateSpace(tight, dir, dir); err == nil || !strings.HasPrefix(err.Error(), "磁盘空间不足，更新尚未开始：") {
		t.Errorf("差 1 字节也应拒绝：%v", err)
	}
	if _, err := planFunasrUpdateSpace(func(string) (int64, int64, error) { return 1, 0, nil }, dir, dir); err == nil || !strings.Contains(err.Error(), "磁盘返回了无效容量") {
		t.Errorf("%v", err)
	}
	f := filepath.Join(dir, "a.txt")
	if _, err := measureFunasrEnvironment(f, 4096); err == nil || err.Error() != "引擎环境不是普通目录，无法安全备份" {
		t.Errorf("%v", err)
	}
}

func TestFunasrIPC(t *testing.T) {
	fx := newFunasrFixture(t)
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.29")
	b := &Bridge{handlers: map[string]handler{}}
	var mu sync.Mutex
	var pushed []string
	fx.l.setPush(func(ch string, payload any) { mu.Lock(); pushed = append(pushed, ch); mu.Unlock() })
	fx.l.registerFunasr(b)
	call := func(ch string, args ...any) any {
		t.Helper()
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i], _ = json.Marshal(a)
		}
		out, err := b.handlers[ch](context.Background(), raw)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		return toGeneric(t, out)
	}
	if st := call("funasr:status").(map[string]any); st["installed"] != false || st["installSupported"] != true {
		t.Errorf("初始状态：%v", st)
	}
	// 安装失败（下载一律 404）；失败后缓存应已失效
	// 安装失败（下载一律 404）
	if res := call("funasr:install").(map[string]any); res["success"] != false || !strings.HasPrefix(fmt.Sprint(res["error"]), "全部下载源失败——") {
		t.Errorf("install 失败：%v", res)
	}
	call("funasr:status")
	// 状态被缓存：装好目录后不 force 仍是旧值
	fx.installLayout("1.3.29")
	if st := call("funasr:status").(map[string]any); st["installed"] != false {
		t.Errorf("缓存期内应沿用旧状态：%v", st)
	}
	if st := call("funasr:status", true).(map[string]any); st["installed"] != true || st["version"] != "1.3.29" {
		t.Errorf("force 应刷新：%v", st)
	}
	fx.setPyPI("1.3.30")
	if up := call("funasr:checkUpdate").(map[string]any); up["current"] != "1.3.29" || up["latest"] != "1.3.30" || up["updateAvailable"] != true {
		t.Errorf("checkUpdate：%v", up)
	}
	// 参数不是字符串：{success:false,error}，不抛异常
	if res := call("funasr:update", 42).(map[string]any); res["success"] != false || res["error"] != "无效的 FunASR 更新版本" {
		t.Errorf("update 非法参数：%v", res)
	}
	if res := call("funasr:update", "1.3.31").(map[string]any); res["success"] != false || res["error"] != "可用版本已变化，请重新检查更新" {
		t.Errorf("update 版本变化：%v", res)
	}
	// 更新成功：结果带 version，进度经 funasr:installProgress 推送
	if err := fx.e.ensure(15 * time.Second); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_FUNASR_VERSION", "1.3.30")
	if res := call("funasr:update", "1.3.30").(map[string]any); res["success"] != true || res["version"] != "1.3.30" || res["warning"] != nil {
		t.Errorf("update 成功：%v", res)
	}
	mu.Lock()
	n := 0
	for _, ch := range pushed {
		if ch == chFunasrProgress {
			n++
		}
	}
	mu.Unlock()
	if n < 4 {
		t.Errorf("进度推送应至少覆盖 4 个阶段：%d", n)
	}
	if st := call("funasr:status").(map[string]any); st["version"] != "1.3.30" {
		t.Errorf("更新后缓存应失效：%v", st)
	}
	if res := call("funasr:uninstall").(map[string]any); res["success"] != true || res["error"] != nil {
		t.Errorf("uninstall：%v", res)
	}
	if st := call("funasr:status").(map[string]any); st["installed"] != false {
		t.Errorf("卸载后状态：%v", st)
	}
}
