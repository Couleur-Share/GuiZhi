package imports

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 导入任务详情弹窗（渲染层 components/imports/ImportTaskDetailModal.tsx）。
//
// 终态任务此前完全没有可点开的形态——点标题会跳去知识库，失败的任务连点都点不了，而它恰恰是最需要看清楚的那种。
// 这里把一条任务的全部可观测信息收在一处：阶段耗时与 AI 开销、来源、时间、报错与缺失提示，
// 外加一颗把这些一次性带走的「复制诊断信息」——用户报「这批采集特别慢」时，双方手上得有数。

// itemFactsEntry 是一个条目事实的读取进度：done 为假表示还在读，found 为假表示读不到（不存在或出错）。
type itemFactsEntry struct {
	done, found bool
	facts       services.ImportItemFacts
}

// detailState 是详情弹窗自己的界面状态。弹窗每次打开都是新的一页（openDetail 复位）。
type detailState struct {
	modal kit.ModalState
	// 阶段耗时的图例：展开全部、悬停中的阶段、命中区的分组登记。只在界面线程读写。
	showAll bool
	hovered string
	hover   kit.HoverGroups

	// 以下字段由后台动作写、界面线程读，受 Screen.mu 保护。
	copying      bool                       // 「复制诊断信息」进行中（按钮禁用）
	webBusy      bool                       // 网页验证重试进行中
	nodePending  bool                       // NodeSeek 验证进行中
	nodeCanceled bool                       // 这次 NodeSeek 验证已被取消：不再重试任务，也不弹失败提示
	facts        map[string]*itemFactsEntry // 条目事实：弹窗打开期间按需读取，关闭时清空
	closeFor     string                     // 后台动作成功后请求关闭的任务 id（渲染层 onComplete）
}

// OpenDetail 打开一个任务的详情弹窗（对照与外壳的入口；行上的「任务详情」等也走它）。
func (s *Screen) OpenDetail(id string) { s.openDetail(id) }

// openDetail 打开任务详情弹窗。弹窗按任务 id 记而不是存整条任务：任务还在跑时会不断有新状态推过来，
// 存快照的话弹窗会停在打开那一刻，越看越不对。
func (s *Screen) openDetail(id string) {
	s.detailTaskID = id
	s.detail.showAll, s.detail.hovered = false, ""
	s.mu.Lock()
	s.detail.facts = nil
	s.detail.copying, s.detail.webBusy, s.detail.closeFor = false, false, ""
	s.mu.Unlock()
}

// closeDetail 关闭弹窗并放掉它的状态。NodeSeek 验证还在等待时一并取消（渲染层卸载时的清理，悄悄取消不弹提示）。
func (s *Screen) closeDetail() {
	if s.detailTaskID == "" {
		return
	}
	s.detailTaskID = ""
	s.detail.showAll, s.detail.hovered = false, ""
	s.mu.Lock()
	cancel := s.detail.nodePending
	if cancel {
		s.detail.nodeCanceled = true
	}
	s.detail.facts = nil
	s.mu.Unlock()
	if cancel {
		s.cancelNodeseek(true)
	}
}

// requestCloseDetail 由后台动作调用：请求界面线程在下一帧关闭 id 这个任务的弹窗。
func (s *Screen) requestCloseDetail(id string) {
	s.mu.Lock()
	s.detail.closeFor = id
	s.mu.Unlock()
	s.Store.Wake()
}

// detailSnapshot 是后台动作写入的那几个状态在这一帧的读数。
type detailSnapshot struct {
	copying, webBusy, nodePending bool
}

func (s *Screen) detailSnapshot() detailSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return detailSnapshot{copying: s.detail.copying, webBusy: s.detail.webBusy, nodePending: s.detail.nodePending}
}

// itemFacts 返回条目 id 的事实：还在读、读不到或出错时返回 false。首次询问时在后台发起读取，读到后请求重绘。
// 渲染层读不到不提示（成果卡有没有都不该让整张任务详情弹窗出错），失败只进日志。
func (s *Screen) itemFacts(id string) (services.ImportItemFacts, bool) {
	if id == "" {
		return services.ImportItemFacts{}, false
	}
	s.mu.Lock()
	entry, asked := s.detail.facts[id]
	if !asked {
		if s.detail.facts == nil {
			s.detail.facts = map[string]*itemFactsEntry{}
		}
		entry = &itemFactsEntry{}
		s.detail.facts[id] = entry
	}
	done, found, facts := entry.done, entry.found, entry.facts
	s.mu.Unlock()
	if !asked {
		s.Store.Spawn(func() {
			loaded, ok, err := s.Store.ItemFacts(s.Ctx, id)
			if err != nil {
				s.Guard.Background(locales.CommonLoadFailed(), fmt.Errorf("import detail item %s: %w", id, err))
				ok = false
			}
			s.mu.Lock()
			// 弹窗关闭（或换了任务）后这次读取作废：登记已被清掉，不再写回。
			if s.detail.facts[id] == entry {
				entry.done, entry.found, entry.facts = true, ok, loaded
			}
			s.mu.Unlock()
			s.Store.Wake()
		})
	}
	return facts, done && found
}

// detailTask 找到弹窗对应的任务：任务被「清理已完成」删掉时找不到，弹窗跟着消失。
func (s *Screen) detailTask(v View) (services.ImportTask, bool) {
	if s.detailTaskID == "" {
		return services.ImportTask{}, false
	}
	for _, task := range v.Tasks {
		if task.ID == s.detailTaskID {
			return task, true
		}
	}
	return services.ImportTask{}, false
}

// detailDialog 声明详情弹窗；每一帧都要经过（弹窗不开的那一帧复位它的状态）。
func (s *Screen) detailDialog(c ui.C, v View, now int64) {
	l := s.Look
	s.mu.Lock()
	closeFor := s.detail.closeFor
	s.detail.closeFor = ""
	s.mu.Unlock()
	if closeFor != "" && closeFor == s.detailTaskID {
		s.closeDetail()
	}
	task, open := s.detailTask(v)
	if !open && s.detailTaskID != "" {
		s.closeDetail()
	}
	spec := kit.ModalSpec{Open: open, Size: kit.ModalXL, CloseLabel: locales.CommonClose()}
	if open {
		spec.Title = task.DisplayName
		if spec.Title == "" {
			spec.Title = task.SourceInput
		}
		spec.Subtitle, spec.SubtitleText = s.detailSubtitle(task)
		snap := s.detailSnapshot()
		spec.HeaderActions = func(c ui.C) {
			b := l.Button(c.Key("copy-report"), kit.ButtonSpec{
				Label: locales.ImportsCopyReport(), Icon: icons.ClipboardCopy, IconSize: l.Rem(0.875), Disabled: snap.copying,
			}, s.actionStyle(actionOutline))
			if b.Clicked() {
				s.copyReport(task)
			}
		}
		spec.Body = func(c ui.C) { s.detailBody(c, task, now) }
	}
	if l.Modal(c.Key("detail"), &s.detail.modal, spec) {
		s.closeDetail()
	}
}

// detailSubtitle 是页眉标题下的副标题：来源平台的品牌图标加平台名（通用网页桶例外：「网页」二字什么都没说，
// 主机名才是这里唯一有信息的东西）；手工粘贴的文本没有来源可言，取所在文件夹。第二个返回值是它的纯文字（对话框的可访问描述）。
func (s *Screen) detailSubtitle(task services.ImportTask) (func(c ui.C), string) {
	l := s.Look
	t := l.T
	platform, hasPlatform := services.ResolveSourcePlatform(task.SourceKind, task.SourceInput)
	if !hasPlatform {
		folder, ok := ResolveTaskFolder(task)
		if !ok {
			return nil, ""
		}
		// mt-0.5 text-xs text-muted-foreground line-clamp-2
		return func(c ui.C) {
			l.Text(c.Key("folder"), folder, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.LineClamp(2)).FillWidth()
		}, folder
	}
	label := kit.PlatformLabel(platform)
	if host, ok := ResolveTaskHost(task); ok && platform == "web" {
		label = host
	}
	// inline-flex items-center gap-1.5：图标 h-3.5 w-3.5 shrink-0，名称 truncate。
	// 行内弹性盒让所在的行盒比一行文字高出一截（见 inlineFlexExtra），外层的列把这一截补上，内容落在上部。
	return func(c ui.C) {
		ui.Column(c.Key("platform-line")).FillWidth().Height(l.Rem(1) + s.inlineFlexExtra(l.Rem(0.875))).Children(func() {
			ui.Row(c.Key("platform")).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
				l.PlatformIcon(c.Key("icon"), platform, l.Rem(0.875), t.MutedForeground).Shrink(0)
				l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).Shrink(1).MinWidth(0)
			})
		})
	}, label
}

// detailBody 是弹窗内容：space-y-5 的几块——字段、成果卡、阶段耗时、报错、缺失提示、来源刷新对照、动作。
func (s *Screen) detailBody(c ui.C, task services.ImportTask, now int64) {
	l := s.Look
	openable := openableItemID(task)
	var completed services.ImportItemFacts
	hasCompleted := false
	if openable != "" {
		completed, hasCompleted = s.itemFacts(openable)
	}
	pair, hasPair := s.refreshPair(task)

	ui.Column(c.Key("detail-body")).FillWidth().Gap(l.Space(5)).Children(func() {
		s.detailFields(c.Key("fields"), task)
		if hasCompleted {
			s.completionCard(c.Key("completion"), completed,
				func() { s.openItemAndClose(completed.ID) },
				func() { s.askAboutAndClose(completed) })
		}
		s.detailStages(c.Key("stages"), task, now)
		if task.Error != "" {
			s.detailError(c.Key("error"), task)
		}
		if task.Warning != "" {
			s.detailWarning(c.Key("warning"), task)
		}
		if hasPair {
			s.refreshCard(c.Key("refresh"), pair)
		}
		if openable != "" || task.Status == services.ImportStatusFailed || task.Status == services.ImportStatusCanceled {
			s.detailActions(c.Key("actions"), task, openable, pair, hasPair)
		}
	})
}

// openItemAndClose 打开条目并关闭弹窗（渲染层 onOpenItem(id); onClose()）。
func (s *Screen) openItemAndClose(itemID string) {
	if s.Hooks.OpenItem != nil {
		s.Hooks.OpenItem(itemID)
	}
	s.closeDetail()
}

// askAboutAndClose 围绕新入库的内容开始提问并关闭弹窗：只预填草稿、不发送，用户能先改成自己的问题，也不会因误点消耗模型调用。
// 当前问答引擎会从全库检索，标题是把召回收窄到这条结果的稳定锚点。
func (s *Screen) askAboutAndClose(item services.ImportItemFacts) {
	if s.Hooks.AskAboutItem != nil {
		title := item.Title
		if title == "" {
			title = locales.LibraryUntitled()
		}
		s.Hooks.AskAboutItem(locales.ImportsAskAboutItemDraft(title))
	}
	s.closeDetail()
}

// field 是一个字段：min-w-0，标签 text-xs 次要色，值 mt-0.5 break-words text-xs 正文色；两列各占一半。
func (s *Screen) field(c ui.C, label string, value func(c ui.C)) {
	l := s.Look
	t := l.T
	ui.Column(c).Grow(1).Basis(0).Shrink(1).MinWidth(0).Children(func() {
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis())
		ui.Column(c.Key("value")).FillWidth().MarginTop(l.Space(0.5)).Children(func() { value(c.Key("content")) })
	})
}

// fieldText 是纯文字的字段值。
func (s *Screen) fieldText(c ui.C, text string) {
	s.Look.Text(c, text, kit.TextXS, kit.WeightNormal, s.Look.T.Foreground, kit.Wrapped()).FillWidth()
}

// fieldRow 是字段网格的一行：grid-cols-2 gap-4，右格可以空着（占位保持左格只有半宽）。
func (s *Screen) fieldRow(c ui.C, left, right func(c ui.C)) {
	ui.Row(c).FillWidth().Gap(s.Look.Space(4)).Items(ui.Start).Children(func() {
		left(c.Key("left"))
		if right != nil {
			right(c.Key("right"))
			return
		}
		ui.Row(c.Key("empty")).Grow(1).Basis(0)
	})
}

// detailFields 是字段网格：提交来源（手机端再加桌面接收时间）、状态、入队、来源、更新。
func (s *Screen) detailFields(c ui.C, task services.ImportTask) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		var received func(c ui.C)
		if task.Origin == OriginMobile && task.ReceivedAt != 0 {
			received = func(c ui.C) {
				s.field(c, locales.ImportsReceivedAt(), func(c ui.C) {
					s.fieldText(c, kit.FormatDateTime(task.ReceivedAt, s.loc(), s.Locale))
				})
			}
		}
		s.fieldRow(c.Key("origin"), func(c ui.C) {
			s.field(c, locales.ImportsOriginFilter(), func(c ui.C) { s.originValue(c, task) })
		}, received)

		// 渲染层里「状态」与「 · 阶段」是同一个字段里相邻的两个文本节点：对照按节点各记一条，所以这里也拆成两段画。
		status := []string{StatusLabel(task.Status)}
		if task.Status == services.ImportStatusProcessing && task.Stage != "" {
			status = append(status, " \u00b7 "+StageLabel(task.Stage))
		}
		s.fieldRow(c.Key("status"), func(c ui.C) {
			s.field(c, locales.ImportsReportStatus(), func(c ui.C) {
				l.TextRuns(c, kit.TextXS, kit.WeightNormal, t.Foreground, status...)
			})
		}, func(c ui.C) {
			s.field(c, locales.ImportsReportQueuedAt(), func(c ui.C) {
				s.fieldText(c, kit.FormatDateTime(task.CreatedAt, s.loc(), s.Locale))
			})
		})
		s.fieldRow(c.Key("source"), func(c ui.C) {
			s.field(c, locales.ImportsReportSource(), func(c ui.C) {
				l.TextBreakAll(c, task.SourceInput, kit.TextXS, kit.WeightNormal, t.Foreground) // break-all：行满处逐字折行
			})
		}, func(c ui.C) {
			s.field(c, locales.ImportsReportUpdatedAt(), func(c ui.C) {
				s.fieldText(c, kit.FormatDateTime(task.UpdatedAt, s.loc(), s.Locale))
			})
		})
	})
}

// originValue 是「提交来源」的值：inline-flex shrink-0 items-center gap-1，图标 h-3 w-3，文字继承正文色。
// 行内弹性盒按自己的基线（图标的下沿）对齐在行盒里，行盒因此比一行文字高出一点：值块的高度补上这一点，见 inlineFlexExtra。
func (s *Screen) originValue(c ui.C, task services.ImportTask) {
	l := s.Look
	ui.Column(c).FillWidth().Height(l.Rem(1) + s.inlineFlexExtra(l.Rem(0.75))).Children(func() {
		s.originLabel(c.Key("label"), task, l.T.Foreground)
	})
}

// detailStages 是「阶段耗时」一节：标题加堆叠条与图例；加统计之前入库的老任务与排队时就被取消的任务没有阶段，写一句说明。
func (s *Screen) detailStages(c ui.C, task services.ImportTask, now int64) {
	l := s.Look
	t := l.T
	stats := ResolveStageStats(task, now)
	ui.Column(c).FillWidth().Children(func() {
		l.Text(c.Key("title"), locales.ImportsReportStages(), kit.TextXS, kit.WeightMedium, t.Foreground).MarginBottom(l.Space(2))
		if len(stats) > 0 {
			s.stageBreakdown(c.Key("breakdown"), stats)
			return
		}
		l.Text(c.Key("none"), locales.ImportsNoStageStats(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).FillWidth()
	})
}

// noteBox 是报错与缺失提示共用的色块：rounded-lg px-3 py-2，标签 text-[11px]，正文 mt-0.5 break-words text-xs。
func (s *Screen) noteBox(c ui.C, bg, labelColor, bodyColor tokens.Color, label, body string) {
	l := s.Look
	ui.Column(c).FillWidth().PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)}) }).
		Children(func() {
			s.stageText(c.Key("label"), label, labelColor)
			l.Text(c.Key("body"), body, kit.TextXS, kit.WeightNormal, bodyColor, kit.Wrapped()).FillWidth().MarginTop(l.Space(0.5))
		})
}

// detailError 是报错块。需要网页验证的任务给出下一步的指引（自动求解开着与否说法不同），其余按错误码翻成人话。
func (s *Screen) detailError(c ui.C, task services.ImportTask) {
	t := s.Look.T
	body := FormatTaskError(task.Error)
	if NeedsWebVerification(task) && !strings.Contains(task.Error, "web_verification_required") {
		body = locales.ImportsWebVerificationRequired()
		if s.webSolverEnabled() {
			body = locales.ImportsWebSolverRequired()
		}
	}
	s.noteBox(c, t.Destructive.WithAlpha(0.1), t.Destructive.WithAlpha(0.8), t.Destructive, locales.ImportsReportError(), body)
}

// detailWarning 是缺失提示块：琥珀色，与「完成（有缺失）」徽标同一套语言。
func (s *Screen) detailWarning(c ui.C, task services.ImportTask) {
	amber := s.tone("amber-600", "amber-400")
	s.noteBox(c, amber500(0.1), amber.WithAlpha(0.8), amber, locales.ImportsReportWarning(), FormatTaskWarning(task.Warning))
}

// webSolverEnabled 报告网页验证自动求解是否开启。
func (s *Screen) webSolverEnabled() bool {
	return s.Hooks.WebSolverEnabled != nil && s.Hooks.WebSolverEnabled()
}

// refreshPair 是来源刷新的对照：刷新完成的任务同时读到原条目与刷新副本时才有；任何一边读不到都不显示。
type refreshPair struct{ original, refreshed services.ImportItemFacts }

func (s *Screen) refreshPair(task services.ImportTask) (refreshPair, bool) {
	if task.RefreshOfItemID == "" || task.Status != services.ImportStatusCompleted || task.ResultItemID == "" {
		return refreshPair{}, false
	}
	original, okOriginal := s.itemFacts(task.RefreshOfItemID)
	refreshed, okRefreshed := s.itemFacts(task.ResultItemID)
	if !okOriginal || !okRefreshed {
		return refreshPair{}, false
	}
	return refreshPair{original: original, refreshed: refreshed}, true
}

// refreshCard 是「来源刷新待确认」：rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-3。
func (s *Screen) refreshCard(c ui.C, pair refreshPair) {
	l := s.Look
	t := l.T
	ui.Column(c).FillWidth().PadSides(l.Space(3)+1, l.Space(3)+1, l.Space(3)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: amber500(0.05), Border: amber500(0.3), BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			l.Text(c.Key("title"), locales.ImportsRefreshComparison(), kit.TextXS, kit.WeightMedium, t.Foreground)
			s.relaxedText(c.Key("hint"), locales.ImportsRefreshComparisonHint(), t.MutedForeground).MarginTop(l.Space(1))
			ui.Row(c.Key("pair")).FillWidth().Gap(l.Space(2)).Items(ui.Start).MarginTop(l.Space(2)).Children(func() {
				s.refreshSide(c.Key("original"), locales.ImportsRefreshOriginal(), pair.original)
				s.refreshSide(c.Key("new"), locales.ImportsRefreshNew(), pair.refreshed)
			})
		})
}

// refreshSide 是对照的一侧：rounded-md bg-background/60 p-2，三行——说明、标题（truncate）、字数。
func (s *Screen) refreshSide(c ui.C, caption string, facts services.ImportItemFacts) {
	l := s.Look
	t := l.T
	title := facts.Title
	if title == "" {
		title = locales.LibraryUntitled()
	}
	ui.Column(c).Grow(1).Basis(0).Shrink(1).MinWidth(0).Pad(l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Radius: l.Rem(0.75) - 2})
		}).
		Children(func() {
			l.Text(c.Key("caption"), caption, kit.TextXS, kit.WeightNormal, t.MutedForeground)
			l.Text(c.Key("title"), title, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).FillWidth().MarginTop(l.Space(0.5))
			// 渲染层是 {n} {单位}：数字、空格、单位三个文本节点，空格不记，所以数字与单位各画一段。
			l.TextRuns(c.Key("count"), kit.TextXS, kit.WeightNormal, t.MutedForeground,
				strconv.Itoa(facts.BodyLength), " ", locales.LibraryWordUnit()).MarginTop(l.Space(0.5))
		})
}
