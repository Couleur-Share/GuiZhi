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
	"sort"
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
	dist := flag.String("dist", "", "spikes/mygo-shell/dist 目录")
	dbPath := flag.String("db", "", "knowledge.db（建议用隔离 fixture）")
	configDir := flag.String("config", "", "config 目录（放 ai-models.json）；不给则 AI 配置读写报错")
	userData := flag.String("userdata", "", "userData 目录（等价 Electron 的 app.getPath(\"userData\")）；不给则取 -db 所在 data 目录的上一级")
	shot := flag.String("shot", "", "启动后截图到该文件并退出")
	wait := flag.Duration("wait", 6*time.Second, "截图前等待时间")
	bench := flag.Bool("bench", false, "基准模式：窗口放到屏幕外，渲染进程出现 topbar-search 后打印 READY，之后一直运行直到被终止")
	aiTest := flag.String("aitest", "", "AI 代理对拍：模拟服务地址（如 http://127.0.0.1:4827）；跑完 -aiscen 里的场景后把结果写到 -aiout 并退出")
	aiScen := flag.String("aiscen", "", "-aitest 的场景文件 scenarios.json")
	aiRunner := flag.String("airunner", "", "-aitest 的运行器 runner.mjs")
	aiOut := flag.String("aiout", "", "-aitest 的结果输出文件")
	flag.Parse()
	if *dist == "" || *dbPath == "" {
		log.Fatal("需要 -dist 与 -db")
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, *dbPath, sqlite.OpenOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	ud := *userData
	if ud == "" {
		ud = filepath.Dir(filepath.Dir(*dbPath))
	}
	lib := &Library{db: db, configDir: *configDir, paths: Paths{UserData: ud}}
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
	lib.relaunch = func() {
		if exe, err := os.Executable(); err == nil {
			_ = exec.Command(exe, os.Args[1:]...).Start()
		}
		mygo.App.Quit()
	}
	lib.jobs = newBackgroundJobRuntime(lib, func(job map[string]any) { lib.pushMain(chBackgroundJobAvail, job) })
	lib.register(b)
	lib.registerList(b)
	lib.registerWrite(b)
	lib.registerCatalogWrite(b)
	lib.registerInboxWrite(b)
	lib.registerAskSessions(b)
	lib.registerAIUsage(b)
	lib.registerSemantic(b)
	lib.registerWiki(b)
	lib.registerKnowledgeDraft(b)
	lib.registerAppRuntime(b)
	lib.registerBackgroundJobs(b)
	lib.registerImport(b)
	if err := lib.imports.Recover(ctx); err != nil { // 恢复上次遗留的导入任务
		log.Printf("[import] 恢复遗留任务失败：%v", err)
	}
	lib.registerInbox(b)
	lib.registerImportList(b)
	lib.registerSettings(b)
	b.registerSimpleBoot(&rendererBackupSettings{})
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
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "归知 GuiZhi（MyGo spike）", Width: 1280, Height: 800, URL: origin + "/",
			Hidden: *shot != "" || *aiTest != "",
			X:      benchX(*bench), Y: benchX(*bench),
			Page: mygo.PageOptions{
				PreloadScript:  string(preload),
				TrustedOrigins: []string{origin},
			},
		})
		b.registerWindow(win)
		p := win.Page()
		lib.setPush(func(channel string, payload any) {
			if err := ipcEvent.EmitPage(p, ipcPush{Channel: channel, Args: []any{payload}}); err != nil {
				log.Printf("[push] 推送 %s 失败：%v", channel, err)
			}
		})
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
	if err := mygo.App.Run(); err != nil {
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
