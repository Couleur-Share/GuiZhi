// 阶段 0 验证原型：归知现有渲染进程 + 未改动的 preload，跑在 MyGo 窗口里。
// Go 侧只有一个绑定服务 Bridge（Invoke/Send），按原 IPC 频道名分发；
// 未实现的频道会记录下来，用来量出“渲染进程启动到底需要多少个频道”。
//
// 用法：go run . -dist <spikes/mygo-shell/dist> -db <fixture/knowledge.db> [-shot out.png] [-wait 6s]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
)

type handler func(ctx context.Context, args []json.RawMessage) (any, error)

// Bridge 是渲染进程唯一的 Go 入口，对应 Electron 的 ipcMain.handle / ipcMain.on。
type Bridge struct {
	mu       sync.Mutex
	handlers map[string]handler
	sends    map[string]func([]json.RawMessage) // ipcRenderer.send 对应的单向频道
	seen     map[string]int                     // 渲染进程调用过的频道及次数
	missing  map[string]int                     // 其中尚未实现的
	db       *sqlite.DB
}

func (b *Bridge) Invoke(ctx context.Context, channel string, args string) (string, error) {
	var list []json.RawMessage
	if args != "" {
		if err := json.Unmarshal([]byte(args), &list); err != nil {
			return "", fmt.Errorf("参数不是 JSON 数组：%w", err)
		}
	}
	b.mu.Lock()
	b.seen[channel]++
	h := b.handlers[channel]
	if h == nil {
		b.missing[channel]++
	}
	b.mu.Unlock()
	if h == nil {
		return "", fmt.Errorf("频道未实现：%s", channel)
	}
	v, err := h(ctx, list)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	return string(out), err
}

// Send 对应 ipcRenderer.send：单向，无返回值。
func (b *Bridge) Send(channel string, args string) {
	b.mu.Lock()
	b.seen["(send) "+channel]++
	h := b.sends[channel]
	b.mu.Unlock()
	if h == nil {
		return
	}
	var list []json.RawMessage
	_ = json.Unmarshal([]byte(args), &list)
	h(list)
}

func (b *Bridge) report() {
	b.mu.Lock()
	defer b.mu.Unlock()
	keys := func(m map[string]int) []string {
		s := make([]string, 0, len(m))
		for k := range m {
			s = append(s, k)
		}
		sort.Strings(s)
		return s
	}
	fmt.Printf("\n渲染进程启动期间调用了 %d 个不同频道，其中 %d 个未实现：\n", len(b.seen), len(b.missing))
	for _, k := range keys(b.seen) {
		mark := "  "
		if b.missing[k] > 0 {
			mark = "✗ "
		}
		fmt.Printf("  %s%-48s ×%d\n", mark, k, b.seen[k])
	}
}

func main() {
	if runCaptureChildIfRequested() { // 采集浏览器子进程：只托管隐藏窗口，不走正常启动流程
		return
	}
	dist := flag.String("dist", "", "spikes/mygo-shell/dist 目录")
	dbPath := flag.String("db", "", "knowledge.db（建议用隔离 fixture）")
	configDir := flag.String("config", "", "config 目录（放 ai-models.json）；不给则 AI 配置读写报错")
	userData := flag.String("userdata", "", "userData 目录（等价 Electron 的 app.getPath(\"userData\")）；不给则取 -db 所在 data 目录的上一级")
	appData := flag.String("appdata", "", "appData 目录（data-path.json 所在的 GuiZhi 上一级）；不给则取 userData 的上一级，避免测试时写到真实 %APPDATA%")
	shot := flag.String("shot", "", "启动后截图到该文件并退出")
	wait := flag.Duration("wait", 6*time.Second, "截图前等待时间")
	bench := flag.Bool("bench", false, "基准模式：窗口放到屏幕外，渲染进程出现 topbar-search 后打印 READY，之后一直运行直到被终止")
	aiTest := flag.String("aitest", "", "AI 代理对拍：模拟服务地址（如 http://127.0.0.1:4827）；跑完 -aiscen 里的场景后把结果写到 -aiout 并退出")
	aiScen := flag.String("aiscen", "", "-aitest 的场景文件 scenarios.json")
	aiRunner := flag.String("airunner", "", "-aitest 的运行器 runner.mjs")
	aiOut := flag.String("aiout", "", "-aitest 的结果输出文件")
	mcpMode := flag.Bool("mcp", false, "以 stdio MCP server 方式运行（供 Cursor / Codex 等 AI IDE 拉起；stdout 仅 JSON-RPC）")
	backupCLI := flag.Bool("backupcli", false, "备份仓库命令行（与 TS 实现对拍/互通验证用，子命令见 backup_cli.go）；之后的位置参数是子命令及其参数")
	flag.Parse()
	if *backupCLI {
		os.Exit(runBackupCLI(flag.Args()))
	}
	if *mcpMode {
		runMcpServer(context.Background(), os.Stdin, os.Stdout, *userData)
		return
	}
	if *dist == "" || *dbPath == "" {
		log.Fatal("需要 -dist 与 -db")
	}

	waitForRelaunchParent()
	automation := *shot != "" || *aiTest != "" || *bench // 自动化实例：不占单实例锁、不建托盘、不抢系统快捷键
	if !automation && !mygo.App.RequestSingleInstanceLock() {
		return // 已有实例在跑，参数已转交给它（它会把窗口带到前台）
	}

	ctx := context.Background()
	ud := *userData
	if ud == "" {
		ud = filepath.Dir(filepath.Dir(*dbPath))
	}
	// 结构初始化/迁移先于插件打开数据库：租约 → 完整性检查 → 升级前备份 → 建表迁移（db_init.go）
	lease, err := initDatabaseFile(ctx, *dbPath, dbInitOptions{BackupsDir: (Paths{UserData: ud}).BackupsDir()})
	if err != nil {
		var live *errLiveDatabaseClient
		if errors.As(err, &live) {
			fmt.Fprintln(os.Stderr, live.Error())
		}
		log.Fatalf("数据库初始化失败：%v", err)
	}
	db, err := sqlite.Open(ctx, *dbPath, sqlite.OpenOptions{})
	if err != nil {
		lease.release()
		log.Fatal(err)
	}
	// 正常退出：先关库，再留「干净关闭」标记（下次启动据此跳过 quick_check），最后释放租约
	shutdownDB := sync.OnceFunc(func() {
		db.Close()
		writeCleanShutdownMarker(*dbPath)
		lease.release()
	})
	defer shutdownDB()

	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	lib := &Library{db: db, dbPath: *dbPath, configDir: *configDir, paths: Paths{UserData: ud}, appData: *appData}
	// 网页渲染：采集浏览器子进程按需启动（首次需要渲染时），空闲 90 s 后自动退出。
	lib.webRenderer = newRenderHost(func() *safeFetcher { return lib.webFetcher })
	if lib.appData == "" {
		lib.appData = filepath.Dir(ud)
	}
	raw, found := lib.readRawSetting(ctx, "networkProxy")
	applyStoredNetworkProxy(raw, found) // 对应 index.ts applyStoredNetworkProxySettings
	lib.backfillMissingFts(ctx)         // 老库里缺的全文索引行（回收站条目、Wiki 页面）补齐
	lib.openDialog = func(multiple bool, name string, exts []string) ([]string, error) {
		return mygo.Dialog.Open(mygo.OpenDialogOptions{Multiple: multiple, Filters: []mygo.FileFilter{{Name: name, Extensions: exts}}})
	}
	lib.pickFiles = func(multiple bool, filters []dialogFilter) ([]string, error) {
		var fs []mygo.FileFilter
		for _, f := range filters {
			fs = append(fs, mygo.FileFilter{Name: f.Name, Extensions: f.Exts})
		}
		return mygo.Dialog.Open(mygo.OpenDialogOptions{Multiple: multiple, Filters: fs})
	}
	lib.pickExecutable = func(title string) (string, error) {
		filters := []mygo.FileFilter{{Name: "All Files", Extensions: []string{"*"}}}
		if runtime.GOOS == "windows" {
			filters = append([]mygo.FileFilter{{Name: "可执行文件", Extensions: []string{"exe"}}}, filters...)
		}
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Filters: filters})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	lib.pickDirectory = func(title string) (string, error) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Directory: true})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	lib.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		var fs []mygo.FileFilter
		for _, f := range filters {
			fs = append(fs, mygo.FileFilter{Name: f.Name, Extensions: f.Exts})
		}
		return mygo.Dialog.Save(mygo.SaveDialogOptions{Title: title, DefaultPath: defaultPath, Filters: fs})
	}
	lib.relaunch = func() {
		if exe, err := os.Executable(); err == nil {
			cmd := exec.Command(exe, os.Args[1:]...)
			cmd.Env = append(os.Environ(), relaunchParentEnv+"="+strconv.Itoa(os.Getpid()))
			_ = cmd.Start()
		}
		mygo.App.Quit()
	}
	lib.jobs = newBackgroundJobRuntime(lib, func(job map[string]any) { lib.pushMain(chBackgroundJobAvail, job) })
	lib.register(b)
	lib.registerList(b)
	lib.registerWrite(b)
	lib.registerKnowledgeCleanup(b)
	lib.registerKnowledgeBatch(b)
	lib.registerCatalogWrite(b)
	lib.registerInboxWrite(b)
	lib.registerAskSessions(b)
	lib.registerAIUsage(b)
	lib.registerSemantic(b)
	lib.registerWiki(b)
	lib.registerWikiCompiler(b)
	lib.registerKnowledgeDraft(b)
	lib.registerAppRuntime(b)
	lib.registerBackgroundJobs(b)
	lib.registerImport(b)
	lib.registerCrawl(b)
	lib.registerWebSnapshot(b)
	lib.registerManagedTools(b)
	lib.registerFunasr(b)
	lib.registerMedia(b)
	lib.registerArticleAsk(b)
	lib.registerThemedReading(b)
	lib.registerExport(b)
	lib.registerMcp(b)
	lib.registerShortcutsIPC(b)
	lib.registerConfigTransfer(b)
	lib.registerLegacyMigration(b)
	lib.registerDataPath(b)
	lib.registerUpdater(b, packagedBuild == "true", mygo.App.Quit)
	lc := newAppLifecycle(runtime.GOOS == "windows")
	lc.setLoginItem = setLoginItemNative
	lc.register(b)
	if err := lib.imports.Recover(ctx); err != nil { // 恢复上次遗留的导入任务
		log.Printf("[import] 恢复遗留任务失败：%v", err)
	}
	lib.registerInbox(b)
	lib.registerImportList(b)
	lib.registerSettings(b)
	renderer := &rendererBackupSettings{}
	lib.registerBackup(b, renderer)
	b.registerSimpleBoot(renderer)
	if !automation {
		lib.jobs.Start() // 对应 backgroundJobs.start()：自动化实例（截图/基准/AI 对拍）不跑后台任务
	}
	newAIProxy(lib.configuredAIHosts).register(b)
	mygo.Bind(b)

	preload, err := os.ReadFile(filepath.Join(*dist, "preload", "preload.js"))
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go http.Serve(ln, http.FileServer(http.Dir(filepath.Join(*dist, "renderer"))))
	origin := "http://" + ln.Addr().String()

	mygo.App.WhenReady(func() {
		manage := !automation // 真实启动：恢复上次窗口、按登录项/设置决定显示或驻留托盘
		stateFile := filepath.Join(ud, "window-state.json")
		launch := windowLaunchState{ShouldMaximize: false, Bounds: windowBounds{Width: 1280, Height: 800}}
		opts := mygo.WindowOptions{
			Title: "归知 GuiZhi（MyGo spike）", Width: 1280, Height: 800, URL: origin + "/",
			Hidden: *shot != "" || *aiTest != "",
			X:      benchX(*bench), Y: benchX(*bench),
			Page: mygo.PageOptions{
				PreloadScript:  string(preload),
				TrustedOrigins: []string{origin},
			},
		}
		if manage {
			launch = readWindowLaunchState(stateFile, displayWorkAreas())
			opts.Width, opts.Height = launch.Bounds.Width, launch.Bounds.Height
			if launch.HasPosition {
				opts.X, opts.Y = launch.Bounds.X, launch.Bounds.Y
			}
			opts.MinWidth, opts.MinHeight = minWindowWidth, minWindowHeight
			opts.Hidden = true // 等 ready-to-show 再按启动可见性决定
			opts.BackgroundColor = "#1a1d23"
			opts.Frameless = runtime.GOOS == "windows" // 与 Electron 版一致：Windows 用无边框窗口，标题栏由页面自绘
		}
		win := mygo.NewWindow(opts)
		b.registerWindow(win)
		p := win.Page()
		skipGlobal := *shot != "" || *aiTest != "" || *bench // 自动化实例不抢用户正在用的系统级快捷键
		if !skipGlobal {
			lib.sc.mu.Lock()
			lib.sc.native = newMygoShortcutNative(lib, win)
			lib.sc.mu.Unlock()
		}
		lib.startShortcuts(skipGlobal)
		lib.setPush(func(channel string, payload any) {
			if err := ipcEvent.EmitPage(p, ipcPush{Channel: channel, Args: []any{payload}}); err != nil {
				log.Printf("[push] 推送 %s 失败：%v", channel, err)
			}
		})
		lc.mu.Lock()
		lc.win, lc.push = mygoHost{win}, lib.pushMain
		if manage {
			lc.tray = &mygoTray{a: lc, lib: lib, locale: systemLocale}
		}
		lc.quit = mygo.App.Quit
		lc.mu.Unlock()
		wireWindowLifecycle(win, lc, lib, manage, stateFile, launch)
		p.OnDidFailLoad(func(e *mygo.LoadError) { fmt.Println("[FAIL] 页面加载失败：", e) })
		if *bench {
			// 与 Electron 版的就绪判据一致：顶栏搜索框出现在 DOM 里
			go func() {
				for {
					if v, err := p.Eval(`!!document.querySelector('[data-testid="topbar-search"]')`); err == nil && v == true {
						fmt.Println("READY")
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}()
		}
		if *aiTest != "" {
			go func() {
				err := runAITest(p, *aiTest, *aiScen, *aiRunner, *aiOut, 120*time.Second)
				if err != nil {
					fmt.Println("[FAIL] aitest：", err)
					os.Exit(1)
				}
				fmt.Println("[ OK ] aitest 结果已写入", *aiOut)
				mygo.App.Quit()
			}()
			return
		}
		if *shot == "" {
			return
		}
		go func() {
			time.Sleep(*wait)
			if v, err := p.Eval(`document.body.innerText.slice(0,200)`); err == nil {
				fmt.Printf("[INFO] 页面文本：%v\n", v)
			}
			if v, err := p.Eval(`document.getElementById('root') ? document.getElementById('root').childElementCount : -1`); err == nil {
				fmt.Printf("[INFO] #root 子元素数：%v\n", v)
			}
			if v, err := p.Eval(`(()=>{const e=document.querySelector(".titlebar-drag");return e?getComputedStyle(e).getPropertyValue("--app-region"):"无 .titlebar-drag"})()`); err == nil {
				fmt.Printf("[INFO] 拖拽区域 --app-region：%v\n", v)
			}
			png, err := win.CapturePage()
			if err == nil {
				err = os.WriteFile(*shot, png, 0o644)
			}
			if err != nil {
				fmt.Println("[FAIL] 截图：", errors.Unwrap(err), err)
			} else {
				fmt.Printf("[ OK ] 截图 %s（%d 字节）\n", *shot, len(png))
			}
			b.report()
			mygo.App.Quit()
		}()
	})
	err = mygo.App.Run()
	if lib.crawl != nil { // 对应 closeWebCapture：先暂停运行中的批次，再关库
		lib.crawl.close(context.Background())
	}
	if lib.funasrEng != nil { // 对应 will-quit：停掉由本进程拉起的转写服务
		lib.funasrEng.stop()
	}
	shutdownDB()
	if err != nil {
		log.Fatal(err)
	}
}

// benchX 基准模式下把窗口挪到所有显示器之外（与 Electron 版的 offscreen 模式对应）。
func benchX(on bool) int {
	if on {
		return -32000
	}
	return 0
}

// wireWindowLifecycle 把窗口事件接到 appLifecycle：可见性推送、关闭策略、全屏推送、状态落盘、启动可见性。
func wireWindowLifecycle(win *mygo.Window, lc *appLifecycle, lib *Library, manage bool, stateFile string, launch windowLaunchState) {
	var persist *windowStatePersister
	if manage {
		persist = &windowStatePersister{win: win, file: stateFile}
		persist.attach()
	}
	win.OnShow(func() { lc.emitVisibility(true) })
	win.OnHide(func() { lc.emitVisibility(false) })
	win.OnMinimize(func() { lc.emitVisibility(false) })
	win.OnRestore(func() { lc.emitVisibility(true) })
	win.OnEnterFullScreen(func() { lc.push("window:fullscreen-changed", true) })
	win.OnLeaveFullScreen(func() { lc.push("window:fullscreen-changed", false) })
	win.OnClose(func(e *mygo.CloseEvent) {
		if persist != nil {
			persist.flush() // 即使这次关闭被拦下（隐藏到托盘），也先保存
		}
		if lc.onClose() {
			e.PreventDefault()
		}
	})
	win.OnClosed(func() {
		lc.mu.Lock()
		lc.win = nil
		lc.mu.Unlock()
		lc.destroyTray()
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { lc.setQuitting() })
	mygo.App.OnSecondInstance(func([]string, string) { lc.onSecondInstance() })
	if !manage {
		return
	}
	win.OnReadyToShow(func() {
		minimizeOnLaunch, _ := lib.readBoolSetting(context.Background(), "minimizeOnLaunch")
		vis := resolveLaunchVisibility(os.Args, mygo.App.WasOpenedAtLogin(), minimizeOnLaunch, launch.ShouldMaximize)
		switch {
		case vis.MaximizeNow:
			win.Maximize()
		case vis.MaximizeOnFirstShow:
			var off func()
			off = win.OnShow(func() {
				off()
				win.Maximize()
			})
		}
		if vis.StartHidden {
			lc.createTray()
			lc.emitVisibility(false)
			return
		}
		win.Show()
		lc.emitVisibility(true)
	})
}
