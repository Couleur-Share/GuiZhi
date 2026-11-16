package island

import (
	"os"
	"strings"
	"sync"
)

// browserArgsVar 是 WebView2 运行时读取「额外浏览器参数」的环境变量（运行时在启动浏览器进程时读取）。
// It is the environment variable the WebView2 runtime reads for extra browser arguments.
const browserArgsVar = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// componentUpdateOff 关掉 WebView2 浏览器进程启动时的组件更新检查。
// 这是 R2「开岛慢 5.5 s」的 W1 绕法：岛的独立环境只在 Open 期间带上它（计划 §10.7、§10.9）。
const componentUpdateOff = "--disable-component-update"

// openMu 串行化「设置环境变量 → Open → 还原」：环境变量是进程级的，
// 两个并发的 Open 交错设置/还原会让变量停在错误的值上。
var openMu sync.Mutex

// appendBrowserArgs 把 args 追加在进程里原有的值之后，返回把变量还原成进入前状态（包括「原本没设」）的函数。
// 只应在一次 WebHost.Open 调用期间持有：Viva 在创建代理环境时会再读取这个变量并追加代理参数，
// 创建调用返回后还原到我们设的值；调用方随后调用返回的函数，把它还原到进入前的值。
//
// Appends args after the existing value and returns a function restoring the exact prior state
// (including "was unset"). Hold it only for the duration of one WebHost.Open call.
func appendBrowserArgs(args string) func() {
	prev, had := os.LookupEnv(browserArgsVar)
	value := args
	if p := strings.TrimSpace(prev); p != "" {
		value = p + " " + args
	}
	_ = os.Setenv(browserArgsVar, value)
	return func() {
		if had {
			_ = os.Setenv(browserArgsVar, prev)
		} else {
			_ = os.Unsetenv(browserArgsVar)
		}
	}
}
