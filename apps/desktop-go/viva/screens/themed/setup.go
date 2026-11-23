package themed

// 生成设置对话框（ThemedReadingSetup.tsx）的表单状态与判断：默认值、能否提交、提交时的选项。

import (
	"unicode/utf16"

	"guizhi-mygo-shell/native/services"
)

// StyleMaxUnits 是「设计要求」的长度上限（UTF-16 码元，与输入框 maxLength 和处理器的校验口径一致）。
const StyleMaxUnits = 2000

// SetupForm 是生成设置对话框里的表单：值类型，改动返回新表单（界面持有当前值）。
type SetupForm struct {
	Research            bool
	ResearchDepth       string
	EnhancedInteraction bool
	Action              string
	Style               string
	GenerateImages      bool
	MaxImages           int

	// 以下是打开对话框时读到的环境，决定默认值与能否提交，不随表单编辑变化。
	Adjusting   bool
	Models      services.ThemedModels
	SearchReady bool // 搜索服务已配置；没读到搜索状态时按未配置
}

// NewSetupForm 建表单。current 非 nil 表示调整已有页面：沿用它的设计要求、增强交互与查证深度，默认只调整设计、不再生图；
// 新建时默认按搜索服务的默认开关决定是否联网，有生图模型就默认配图。search 为 nil 表示没读到搜索状态。
func NewSetupForm(current *services.ThemedOptions, models services.ThemedModels, search *services.ThemedSearchStatus) SetupForm {
	f := SetupForm{
		ResearchDepth: services.ThemedDepthStandard, EnhancedInteraction: true, Action: services.ThemedActionCreate,
		MaxImages: 3, Models: models,
		GenerateImages: current == nil && models.Image != nil,
	}
	if search != nil {
		f.SearchReady = search.Configured
		f.Research = search.DefaultEnabled
	}
	if current != nil {
		f.Adjusting = true
		f.Action = services.ThemedActionRedesign
		f.Style = current.Style
		f.EnhancedInteraction = current.Enhanced()
		if current.ResearchDepth != "" {
			f.ResearchDepth = current.ResearchDepth
		}
	}
	return f
}

// ShowsResearch 报告联网相关控件是否出现：仅调整设计时不涉及内容，不联网。
func (f SetupForm) ShowsResearch() bool { return f.Action != services.ThemedActionRedesign }

// ResearchMissingSearch 报告是否要提示「请先配置联网搜索」：勾了联网但搜索服务未配置。
func (f SetupForm) ResearchMissingSearch() bool {
	return f.ShowsResearch() && f.Research && !f.SearchReady
}

// Configured 报告能否提交：有主文本模型；联网需要已配置搜索（仅调整设计时不要求）；配图需要生图模型。
func (f SetupForm) Configured() bool {
	return f.Models.Text != nil &&
		(!f.Research || f.Action == services.ThemedActionRedesign || f.SearchReady) &&
		(!f.GenerateImages || f.Models.Image != nil)
}

// NeedsConfigHint 报告是否显示「去配置模型」的提示条：缺文本模型、缺生图模型或联网缺搜索服务。
func (f SetupForm) NeedsConfigHint() bool {
	return f.Models.Text == nil || f.Models.Image == nil || (f.Research && !f.SearchReady)
}

// ConfigHintImageOptional 报告提示条该说「仍可仅排版」（已满足提交条件，只是没配生图模型）还是「请先配置」。
func (f SetupForm) ConfigHintImageOptional() bool { return f.Configured() }

// WithAction 切换调整方式（只对调整模式有意义；非法取值忽略）。
func (f SetupForm) WithAction(action string) SetupForm {
	switch action {
	case services.ThemedActionRedesign, services.ThemedActionRevise, services.ThemedActionRefresh, services.ThemedActionCreate:
		f.Action = action
	}
	return f
}

// WithResearch 勾选 / 取消联网补充资料。
func (f SetupForm) WithResearch(on bool) SetupForm { f.Research = on; return f }

// WithResearchDepth 设置查证深度（非法取值忽略）。
func (f SetupForm) WithResearchDepth(depth string) SetupForm {
	if depth == services.ThemedDepthStandard || depth == services.ThemedDepthDeep {
		f.ResearchDepth = depth
	}
	return f
}

// WithEnhancedInteraction 开关增强交互。
func (f SetupForm) WithEnhancedInteraction(on bool) SetupForm { f.EnhancedInteraction = on; return f }

// WithGenerateImages 开关主题图片。
func (f SetupForm) WithGenerateImages(on bool) SetupForm { f.GenerateImages = on; return f }

// WithMaxImages 设置最多新增图片数，限制在 1 到 ThemedMaxImages。
func (f SetupForm) WithMaxImages(n int) SetupForm {
	f.MaxImages = max(1, min(n, services.ThemedMaxImages))
	return f
}

// WithStyle 设置设计要求，超过上限的部分截掉（不拆开代理对）。
func (f SetupForm) WithStyle(style string) SetupForm {
	f.Style = ClampStyle(style)
	return f
}

// ClampStyle 把设计要求截到 StyleMaxUnits 个 UTF-16 码元以内，不会把一个字符截成半个。
func ClampStyle(style string) string {
	units := utf16.Encode([]rune(style))
	if len(units) <= StyleMaxUnits {
		return style
	}
	cut := StyleMaxUnits
	if utf16.IsSurrogate(rune(units[cut-1])) && cut < len(units) && units[cut-1] < 0xDC00 {
		cut-- // 高代理项落在边界上：丢掉它，保留完整字符
	}
	return string(utf16.Decode(units[:cut]))
}

// Options 是提交时发给服务的选项（与渲染层一致：联网、查证深度、增强交互与调整方式总是显式给值；
// 仅调整设计与调整内容沿用当前页面的内容快照与素材，即 FromCurrent）。
func (f SetupForm) Options() services.ThemedOptions {
	enhanced := f.EnhancedInteraction
	images := f.GenerateImages
	return services.ThemedOptions{
		Style:               f.Style,
		GenerateImages:      images,
		MaxImages:           f.MaxImages,
		Research:            f.Research,
		ResearchDepth:       f.ResearchDepth,
		EnhancedInteraction: &enhanced,
		Action:              f.Action,
		FromCurrent:         f.Action == services.ThemedActionRedesign || f.Action == services.ThemedActionRevise,
	}
}
