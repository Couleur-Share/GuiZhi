package themed

// 阅读页文档的读取：当前格式页面（v3）与生成途中的预览，由外壳按「导出静态 HTML」同一条路径渲染成整页文档
// （services.ThemedReading.Render），交给阅读岛的沙箱 iframe（reader.ViewSnapshot）显示。
// 旧格式页面的文档随读取结果一起到（ReadResult.Document），不经这里。

import (
	"context"
	"strconv"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// DocView 是渲染好的页面文档的快照。
type DocView struct {
	// Key 是这份文档对应的「想要什么」；与调用方要的不同就说明还在换。
	Key string
	// Doc 是整页 HTML；读取新文档期间保留上一份（不闪空白），换成功才替换。
	Doc       string
	VersionID string
	// ImagesOmitted 为 true 表示部分素材还没就绪、文档里没有放图（界面据此说明）。
	ImagesOmitted bool
	// Loading 为首次读取（还没有任何文档）；Error 非空时必须渲染成可重试的错误态（Retry），不能当作空白页。
	Loading bool
	Error   string
}

// DocLoader 读取并缓存页面文档。方法可从任意 goroutine 调用。
//
// 界面每一帧都用「想要的文档」调一次 Want：想要的没变就直接返回快照，变了就作废在途的读取、后台重读。
// 读取失败后不会自己重试（否则每帧一次请求）：界面给「重试」入口，调 Retry。
type DocLoader struct {
	svc        services.ThemedReading
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行读取；默认开 goroutine，测试注入同步执行。
	Spawn func(fn func())

	mu   sync.Mutex
	view DocView
	// gen 让旧读取的结果作废；want 是最近一次想要的文档，Retry 据此重读。
	gen     int
	want    services.ThemedRenderInput
	key     string
	content string // 条目与来源：换了它就清掉上一份文档
}

// NewDocLoader 创建文档读取器。
func NewDocLoader(svc services.ThemedReading, guard kit.Guard, invalidate func()) *DocLoader {
	return &DocLoader{svc: svc, guard: guard, invalidate: invalidate, Spawn: func(fn func()) { go fn() }}
}

// DocKey 是「想要的文档」的身份：条目与来源、版本（或预览的任务与修订号）、阅读实例。
// updatedAt 让同一版本被更新（例如重做了一张图）后重新渲染。
func DocKey(in services.ThemedRenderInput, updatedAt int64, revision int) string {
	return in.ItemID + "\x00" + in.SourceKind + "\x00" + in.VersionID + "\x00" + in.TaskID + "\x00" +
		strconv.FormatInt(updatedAt, 10) + "\x00" + strconv.Itoa(revision) + "\x00" + in.InstanceID
}

func (d *DocLoader) ctx() context.Context {
	if d.Ctx != nil {
		return d.Ctx
	}
	return context.Background()
}

func (d *DocLoader) wake() {
	if d.invalidate != nil {
		d.invalidate()
	}
}

// View 返回当前快照。
func (d *DocLoader) View() DocView {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.view
}

// Want 声明这一帧想要的文档并返回当前快照。key 变了就开始后台读取（读取期间快照仍是上一份文档，Key 与 key 不同）。
func (d *DocLoader) Want(in services.ThemedRenderInput, key string) DocView {
	d.mu.Lock()
	if key == d.key {
		v := d.view
		d.mu.Unlock()
		return v
	}
	gen := d.start(in, key)
	v := d.view
	d.mu.Unlock()
	d.dispatch(in, key, gen)
	return v
}

// start 在持锁时切换到新的目标：换了文章就清掉上一份（不能把上一篇的页面留在画面上）；
// 同一篇里换版本（预览到成品）则保留画面，直到新文档到。
func (d *DocLoader) start(in services.ThemedRenderInput, key string) int {
	d.gen++
	d.key, d.want = key, in
	if content := in.ItemID + "\x00" + in.SourceKind; content != d.content {
		d.content, d.view = content, DocView{}
	}
	d.view.Error = ""
	d.view.Loading = d.view.Doc == ""
	return d.gen
}

func (d *DocLoader) dispatch(in services.ThemedRenderInput, key string, gen int) {
	spawn := d.Spawn
	if spawn == nil {
		spawn = func(fn func()) { go fn() }
	}
	spawn(func() { d.load(in, key, gen) })
	d.wake()
}

func (d *DocLoader) load(in services.ThemedRenderInput, key string, gen int) {
	out, err := d.svc.Render(d.ctx(), in)
	d.mu.Lock()
	if gen != d.gen {
		d.mu.Unlock()
		return // 已被更新的目标取代
	}
	if err != nil {
		d.view.Error, d.view.Loading = err.Error(), false
		d.mu.Unlock()
		d.guard.Background("themedReading.render", err)
		d.wake()
		return
	}
	d.view = DocView{Key: key, Doc: out.Document, VersionID: out.VersionID, ImagesOmitted: out.ImagesOmitted}
	d.mu.Unlock()
	d.wake()
}

// Retry 按最近一次想要的文档重读（读取失败后的重试入口）。
func (d *DocLoader) Retry() {
	d.mu.Lock()
	in, key := d.want, d.key
	if key == "" {
		d.mu.Unlock()
		return
	}
	d.gen++
	gen := d.gen
	d.view.Error, d.view.Loading = "", d.view.Doc == ""
	d.mu.Unlock()
	d.dispatch(in, key, gen)
}

// Reset 清空（切换内容、离开阅读时）：作废在途的读取，下次 Want 重新开始。
func (d *DocLoader) Reset() {
	d.mu.Lock()
	d.gen++
	d.key, d.want, d.content, d.view = "", services.ThemedRenderInput{}, "", DocView{}
	d.mu.Unlock()
}

// ErrorTitle 是文档读取失败时错误态的标题。
func ErrorTitle() string { return locales.ThemedReadingReaderViewFailed() }
