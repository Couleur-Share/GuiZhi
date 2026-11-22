package settings

import (
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Capture 是「采集与转写」页（渲染层 CaptureSettings）：「采集」分区卡片（平台登录账号、yt-dlp、ffmpeg、本地转写引擎、
// 说话人分离开关）加其下的 FlareSolverr 后备采集块。
//
// 三个引擎的安装、更新、移除与平台登录都经 services.CaptureTools 交给主包已有的处理器；这一层只管界面、进度与失败提示。
// 可执行文件路径只能由「选择文件」写入（外壳校验），所以自定义路径框是只读的。
type Capture struct {
	Look  *kit.Look
	Store Store
	// Guard 接住保存与各动作的失败：错误提示（原因折叠在「查看详情」里）并记日志。
	Guard kit.Guard
	// Service 是采集工具服务；nil 表示外壳没接：各行如实显示「检测失败」，动作如实报错。
	Service services.CaptureTools
	// Solver 是 FlareSolverr 连通性测试服务（挂载的 FlareSolverr 块用）；nil 时点测试如实报错。
	Solver services.FlareSolverr
	// Platform 是运行平台（Node 写法，"win32" 等）；空与 "win32" 之外说话人分离不可用。空表示尚未读到，按可用处理（渲染层同样）。
	Platform string
	// Cache 是引擎状态缓存（外壳持有，切换分区不丢）；nil 时只在本页存活期内缓存。
	Cache *EngineCache
	// Now 给本地转写引擎更新耗时计时；nil 取系统时间。
	Now func() time.Time
	// Wake 请求窗口重绘：后台结果回到界面线程前调用。可为 nil。
	Wake func()

	accounts platformAccounts
	ytdlp    *binaryEngine
	ffmpeg   *binaryEngine
	funasr   funasrEngine
	flare    FlareSolverr
}

// Close 在分区被切走时调用：取消订阅与读取请求，丢弃还没送达的结果；进行中的安装与登录不被打断（结果提示照常弹出）。
func (p *Capture) Close() {
	p.accounts.Close()
	if p.ytdlp != nil {
		p.ytdlp.Close()
	}
	if p.ffmpeg != nil {
		p.ffmpeg.Close()
	}
	p.funasr.Close()
	p.flare.Close()
}

// diarizeAvailable 与本地转写引擎的 installSupported 对齐：仅 Windows 有本地引擎（渲染层同样）。
func (p *Capture) diarizeAvailable() bool { return p.Platform == "" || p.Platform == "win32" }

// bind 把本帧的依赖交给各行。
func (p *Capture) bind() {
	l := p.Look
	if p.Cache == nil {
		p.Cache = &EngineCache{}
	}
	if p.ytdlp == nil {
		p.ytdlp = &binaryEngine{
			Tool: services.ToolYtDlp, Texts: ytDlpTexts,
			PathOf:  func(a settings.App) string { return a.YtDlpPath },
			SetPath: func(v *settings.Values, path string) { v.App.YtDlpPath = path },
		}
		p.ffmpeg = &binaryEngine{
			Tool: services.ToolFfmpeg, Optional: true, Texts: ffmpegTexts,
			PathOf:  func(a settings.App) string { return a.FfmpegPath },
			SetPath: func(v *settings.Values, path string) { v.App.FfmpegPath = path },
		}
	}
	p.ytdlp.bind(l, p.Store, p.Guard, p.Service, p.Wake, p.Cache)
	p.ffmpeg.bind(l, p.Store, p.Guard, p.Service, p.Wake, p.Cache)
	p.funasr.Now = p.Now
	p.funasr.bind(l, p.Guard, p.Service, p.Wake, p.Cache)
	p.accounts.look, p.accounts.guard, p.accounts.svc, p.accounts.wake = l, p.Guard, p.Service, p.Wake
	p.flare.Look, p.flare.Store, p.flare.Guard, p.flare.Solver, p.flare.Wake = l, p.Store, p.Guard, p.Solver, p.Wake
}

// Page 声明整页：标题、「采集」分区与 FlareSolverr 块。
func (p *Capture) Page(c ui.C) {
	l := p.Look
	p.bind()
	l.SettingsContent(c, locales.SettingsCaptureAndTranscription(), func() {
		l.SettingSections(c, func() {
			l.SettingSectionBody(c, locales.SettingsCaptureSection(), func() {
				p.accounts.Rows(c.Key("accounts"))
				p.ytdlp.Row(c.Key("ytdlp"))
				p.ffmpeg.Row(c.Key("ffmpeg"))
				p.funasr.Row(c.Key("funasr"))
				p.diarize(c.Key("diarize"))
			})
			p.flare.Page(c.Key("flaresolverr"))
		})
	})
}

// diarize 是「采集时区分说话人」开关：转写会慢一倍，且只有内置本地引擎支持；本平台不支持时开关禁用并显示为关。
func (p *Capture) diarize(c ui.C) {
	l := p.Look
	available := p.diarizeAvailable()
	desc := locales.SettingsTranscribeDiarizeDesc()
	if !available {
		desc = locales.SettingsTranscribeDiarizeUnavailable()
	}
	l.SettingItem(c, kit.SettingRow{
		Label: locales.SettingsTranscribeDiarize(), Description: desc,
		Control: func() {
			on := available && p.Store.Values().App.TranscribeDiarize
			sw := l.SwitchWith(c, &on, kit.SwitchSpec{Label: locales.SettingsTranscribeDiarize(), Disabled: !available})
			if sw.Clicked() && available {
				next := on
				save(p.Guard, p.Store, func(v *settings.Values) { v.App.TranscribeDiarize = next })
			}
		},
	}, true)
}
