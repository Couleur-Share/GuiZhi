// Package native 是归知原生界面（MyGo 原生控件，不加载 WebView 与渲染进程）的根目录，自身没有运行时代码。
// Package native is the root of the GuiZhi native UI (MyGo native widgets; no WebView, no renderer process).
//
// 目录分层（依赖只能向下，禁止反向 import，更不能 import 主包）：
//
//	app        窗口外壳：导航栏、路由、外观装配
//	screens/*  各功能页（采集、知识库…），只依赖 services 接口与 kit
//	kit        共享控件与主题桥（令牌、Look、动效、壁纸、图标）
//	services   各页需要的后端能力接口；主包里的适配器把它们接到真实的处理器
//	locales    文案目录（由 gen-native-i18n.mjs 生成）
//
// 本目录的测试（go test ./native/...）不得启动 WebView2。
// 另有 nocjk_test.go：界面文案必须经 locales，不得在 Go 代码里写中文字面量。
package native
