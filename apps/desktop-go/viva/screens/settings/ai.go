package settings

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// errAIUnavailable 是外壳没有接上 AI 配置服务时的失败原因（进「查看详情」与日志，不进界面文案）。
var errAIUnavailable = errors.New("the AI settings service is not available in this build")

// aiLayoutBreakpoint 是 Tailwind lg 断点（1024px 窗口宽度）：宽于它时左侧栏与内容并排，否则上下叠放。
const aiLayoutBreakpoint = 1024

// aiPanel 是右侧当前展示的面板（EndpointsSection 的 activePanel）。
type aiPanel uint8

const (
	panelRouting  aiPanel = iota // 模型路由：状态总览、场景默认模型、用量
	panelProvider                // 选中的供应商：凭据与模型列表
)

// endpointStatus 是端点卡上的状态（EndpointStatus）。
type endpointStatus struct {
	tone          aiTone
	label, detail string
}

// aiSaveJob 是排队等待写盘的一次修改：cfg 是这次修改之后的整份配置。
type aiSaveJob struct {
	cfg services.AIConfig
	ok  string // 写盘成功后的提示；空表示不提示
	// done 非 nil 时由它接管结果（向导要在弹窗里显示失败原因）：失败不再弹通用错误提示，成功不再弹 ok。
	done func(err error)
}

// AI 是「AI 模型服务」分区（渲染层 AISettingsPrototype 与 ai-workbench / ai-quick-setup）：
// 左侧是模型路由入口与供应商列表，右侧是模型路由（状态总览、场景默认模型、用量）或选中供应商的凭据与模型。
//
// 配置整体读写（config/ai-models.json，经 Service）：每次修改先在内存里乐观生效，再排队整体写盘；写盘失败则回到上一次成功写入的配置，
// 并弹出带原因的错误提示。API Key 只在输入框里（默认圆点）与配置里出现，界面文字、提示、日志里一律不出现明文。
type AI struct {
	Look  *kit.Look
	Guard kit.Guard
	// Service 是 AI 配置服务；nil 表示外壳没接：页面如实显示「读取失败」。
	Service services.AISettings
	// Wake 请求窗口重绘：后台结果回到界面线程前调用。可为 nil。
	Wake func()
	// OnManageEngine 是内置转写引擎卡上的「管理引擎」：跳到「采集与转写」分区。可为 nil。
	OnManageEngine func()
	// Locale 是时间的区域写法（系统区域）；零值按中文。
	Locale kit.DateLocale
	// Now 给连通性验证时间戳；nil 取系统时间。
	Now func() time.Time

	started  bool
	loaded   bool
	loadErr  string
	cfg      services.AIConfig // 界面看到的配置（含尚在排队的修改）
	saved    services.AIConfig // 最近一次确认写盘成功的配置
	w        worker
	jobs     []aiSaveJob
	inflight bool

	panel    aiPanel
	selected string // 选中的端点分组键
	search   ui.Field
	cred     aiCredentials

	statuses        map[string]endpointStatus // 本次会话里测试连接得到的状态
	testingDefault  bool
	testingModel    string
	testingEndpoint string

	endpoint      *endpointForm
	model         *modelForm
	fetch         *fetchState
	wizard        *wizardState
	delModel      *services.AIModel
	delEndpoint   *services.AIEndpointGroup
	delModelDlg   kit.ConfirmState
	delEndpointDl kit.ConfirmState
	routeSel      [6]kit.SelectState
	// 四个弹窗的状态必须常驻：Modal 要在关闭的那一帧复位自己登记的焦点作用域。
	epModal, mdModal, ftModal, wzModal kit.ModalState
	usage                              aiUsage
}

// Close 在分区被切走时调用：取消读取与测试请求、丢弃还没送达的结果；排队中与进行中的写盘不被打断。
// 弹窗与向导的状态随页面对象一起丢弃（回到分区时重新开始，与渲染层切分区即卸载一致）。
func (a *AI) Close() {
	a.w.close()
	a.endpoint, a.model, a.fetch, a.wizard = nil, nil, nil, nil
	a.delModel, a.delEndpoint = nil, nil
}

func (a *AI) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// providerLabel 是供应商标识的显示名：自定义、智谱、通义、豆包随界面语言，其余是品牌名。
func providerLabel(id string) string {
	switch id {
	case "custom":
		return locales.SettingsAiProviderCustom()
	case "zhipu":
		return locales.SettingsAiProviderZhipu()
	case "qwen":
		return locales.SettingsAiProviderQwen()
	case "doubao":
		return locales.SettingsAiProviderDoubao()
	}
	return services.DefaultProviderLabel(id)
}

// notify 弹一条没有详情的提示；没有提示通道时什么也不做。
func (a *AI) notify(kind kit.ToastKind, message string) {
	if a.Guard.Notify != nil {
		a.Guard.Notify.Show(kind, message)
	}
}

// ── 读取 ──

// reload 读取整份配置；读不出来和「还没配置」在界面上长得一样，所以失败要画出来（LoadErrorState）。
func (a *AI) reload() {
	svc := a.Service
	a.loadErr = ""
	a.w.load(a.Wake, func(ctx context.Context) func() {
		cfg, err := services.AIConfig{}, errAIUnavailable
		if svc != nil {
			cfg, err = svc.Load(ctx)
		}
		if err != nil && ctx.Err() == nil {
			a.Guard.Background(locales.SettingsAi(), err)
		}
		return func() {
			if err != nil {
				a.loadErr = err.Error()
				return
			}
			a.cfg, a.saved, a.loaded, a.loadErr = cfg, cfg.Clone(), true, ""
		}
	})
}

// ── 写盘 ──

// commit 把修改后的配置乐观地放进界面并排队写盘；ok 是写盘成功后的提示（空则不提示）。
func (a *AI) commit(next services.AIConfig, ok string) { a.commitThen(next, ok, nil) }

// commitThen 同 commit，但写盘结果交给 done（见 aiSaveJob.done）。
func (a *AI) commitThen(next services.AIConfig, ok string, done func(error)) {
	a.cfg = next
	a.jobs = append(a.jobs, aiSaveJob{cfg: next, ok: ok, done: done})
	a.pump()
}

// pump 一次只让一个写盘在途，按提交顺序逐个写：后一次修改是在前一次之上做的，乱序会把新值写回旧值。
// 某次写盘失败时，排在它后面的修改都是在失败的状态上做的，一并丢弃并回到上一次成功写入的配置。
func (a *AI) pump() {
	if a.inflight || len(a.jobs) == 0 {
		return
	}
	job := a.jobs[0]
	a.jobs = a.jobs[1:]
	a.inflight = true
	svc, g := a.Service, a.Guard
	a.w.act(a.Wake, func() func() {
		err := errAIUnavailable
		if svc != nil {
			err = svc.Save(context.Background(), job.cfg)
		}
		switch {
		case err != nil && job.done == nil:
			g.Report(locales.NativeSettingsSaveAction(), err)
		case err == nil && job.ok != "" && job.done == nil:
			g.Succeed(job.ok)
		}
		return func() {
			a.inflight = false
			if err != nil {
				a.cfg, a.jobs = a.saved.Clone(), nil
				if job.done != nil {
					job.done(err)
				}
				return
			}
			a.saved = job.cfg.Clone()
			if job.done != nil {
				job.done(nil)
			}
			a.pump()
		}
	})
}

// ── 页面 ──

// Page 声明整页。
func (a *AI) Page(c ui.C) {
	l := a.Look
	a.w.drain()
	if !a.started {
		a.started = true
		a.reload()
		a.loadUsage(false)
	}
	switch {
	case a.loadErr != "":
		l.SettingsContent(c, locales.SettingsAi(), func() {
			if l.LoadErrorState(c, a.loadErr) {
				a.reload()
			}
		})
	case !a.loaded:
		l.SettingsContent(c, locales.SettingsAi(), func() { spinnerBox(l, c) })
	default:
		a.workbench(c)
		a.dialogs(c)
	}
}

// groups 是当前的端点分组，同时维护选中项：分组变化后选中的键不存在就回到第一个（EndpointsSection 的 effect）。
func (a *AI) groups() []services.AIEndpointGroup {
	groups := a.cfg.EndpointGroups()
	switch {
	case len(groups) == 0:
		a.selected = ""
	case a.selectedGroup(groups) == nil:
		a.selected = groups[0].Key
	}
	return groups
}

func (a *AI) selectedGroup(groups []services.AIEndpointGroup) *services.AIEndpointGroup {
	for i := range groups {
		if groups[i].Key == a.selected {
			return &groups[i]
		}
	}
	return nil
}

// workbench 是左侧栏加右侧面板；窄窗口上下叠放。
func (a *AI) workbench(c ui.C) {
	l := a.Look
	groups := a.groups()
	wide := l.ViewportWidth(c) >= aiLayoutBreakpoint
	ui.Column(c).Fill().Draw(func(d *ui.DrawContext) {
		d.Canvas.Fill(d.Rect, d.Fade(l.Col(l.T.UiSectionBg)))
	}).Children(func() {
		if a.hasLegacyOnlyConfig() {
			a.legacyBanner(c.Key("legacy"))
		}
		a.workbenchBody(c.Key("body-row"), groups, wide)
	})
}

// workbenchBody 是左侧栏加右侧面板。
func (a *AI) workbenchBody(c ui.C, groups []services.AIEndpointGroup, wide bool) {
	l := a.Look
	box := ui.Row(c)
	if !wide {
		box = ui.Column(c)
	}
	box.FillWidth().Grow(1).MinHeight(0).Children(func() {
		a.aside(c.Key("aside"), groups, wide)
		l.ScrollArea(c.Key("main"), [4]float32{l.Space(5), l.Space(6), l.Space(5), l.Space(6)}, func() {
			ui.Column(c.Key("body")).FillWidth().Children(func() {
				l.Heading(c, 1, locales.SettingsAi(), kit.TextLG, kit.WeightSemibold, l.T.Foreground).MarginBottom(l.Space(4))
				if g := a.selectedGroup(groups); a.panel == panelProvider && g != nil {
					a.endpointCard(c.Key("endpoint"), *g)
				} else {
					a.routing(c.Key("routing"), groups)
				}
			})
		}).Grow(1).MinWidth(0)
	})
}

// dialogs 声明所有弹窗：每一帧都要经过，状态复位由各控件自己负责。
func (a *AI) dialogs(c ui.C) {
	a.deleteDialogs(c.Key("delete"))
	a.endpointModal(c.Key("endpoint-form"))
	a.modelModal(c.Key("model-form"))
	a.fetchModal(c.Key("model-fetch"))
	a.wizardModal(c.Key("wizard"))
}

// trimmed 去掉首尾空白。
func trimmed(s string) string { return strings.TrimSpace(s) }
