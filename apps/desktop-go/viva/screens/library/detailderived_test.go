package library

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// 本文件测详情的派生值缓存（detailderived.go）：结果与直接计算一致、输入不变就不重算、读到的每个字段变了都重算，
// 并守住「这三个函数只经缓存调用」（正文有几 MB 时逐帧重算会让稳态帧多出 2 ms 以上）。

// derivedCorpus 是覆盖各条分支的条目：普通笔记、公众号、带文字稿的视频、论坛、图片、空白笔记、回收站里的、含增补平面字符的。
func derivedCorpus() []services.LibraryItem {
	return []services.LibraryItem{
		{ID: "note", ItemType: services.LibraryTypeNote, Content: "# 标题\n\n正文内容。", SourceURI: "https://example.com/a"},
		{ID: "wechat", ItemType: services.LibraryTypeNote, Content: "正文", SourceURI: "https://mp.weixin.qq.com/s/abc"},
		{ID: "video", ItemType: services.LibraryTypeVideo, Content: "视频", Transcript: " 这是一段文字稿内容。 ", SourceURI: "https://www.bilibili.com/video/BV1xx"},
		{ID: "forum", ItemType: services.LibraryTypeForum, Content: detailForumContent, SourceURI: "https://linux.do/t/123"},
		{ID: "image", ItemType: services.LibraryTypeImage, Content: "文案段落\n\n## 图中文字\n\n识别出的字\n"},
		{ID: "blank", ItemType: services.LibraryTypeNote, Content: " \n "},
		{ID: "trash", ItemType: services.LibraryTypeNote, Content: "已删除", DeletedAt: 5, SourceURI: "https://example.com/t"},
		{ID: "astral", ItemType: services.LibraryTypeNote, Content: "😀 表情与文字"},
	}
}

func TestDerivedValuesMatchDirectComputation(t *testing.T) {
	t.Parallel()
	for _, item := range derivedCorpus() {
		var s Screen
		d := s.derivedOf(item)
		if want := deriveCaps(item); !reflect.DeepEqual(d.caps, want) {
			t.Errorf("%s：能力 %+v，直接算是 %+v", item.ID, d.caps, want)
		}
		if want := wordCount(item.Content); d.words != want {
			t.Errorf("%s：字数 %d，直接算是 %d", item.ID, d.words, want)
		}
		if want := sourceViewOf(item); !reflect.DeepEqual(d.source, want) {
			t.Errorf("%s：来源 %+v，直接算是 %+v", item.ID, d.source, want)
		}
	}
}

func TestDerivedValuesAreRecomputedOnlyWhenTheirInputsChange(t *testing.T) {
	t.Parallel()
	var s Screen
	base := services.LibraryItem{ID: "a", Title: "标题", ItemType: services.LibraryTypeNote, Content: "# 正文\n\n内容", SourceURI: "https://example.com/a"}
	computes := func() int { return s.detail.derived.computes }

	s.derivedOf(base)
	if computes() != 1 {
		t.Fatalf("第一次应算一次，实际 %d 次", computes())
	}
	for i := 0; i < 5; i++ {
		s.derivedOf(base)
	}
	if computes() != 1 {
		t.Fatalf("同一份输入问 6 次应只算一次，实际 %d 次", computes())
	}

	// 不在输入里的字段（标题、标签、收藏、置顶、更新时间、ID）变了：不重算。
	edited := base
	edited.ID, edited.Title, edited.IsFavorite, edited.IsPinned, edited.UpdatedAt = "b", "新标题", true, true, 99
	edited.Tags = []services.LibraryTag{{ID: "t1", Name: "标签"}}
	s.derivedOf(edited)
	if computes() != 1 {
		t.Fatalf("标题、标签等不在输入里的字段变了不该重算，实际 %d 次", computes())
	}

	// 输入里的每个字段：改一个就重算一次，结果跟着变。
	for _, c := range []struct {
		name  string
		edit  func(*services.LibraryItem)
		check func(*contentDerived) bool
	}{
		{"正文", func(it *services.LibraryItem) { it.Content = "# 正文\n\n内容更多一点点" }, func(d *contentDerived) bool { return d.words == wordCount("# 正文\n\n内容更多一点点") }},
		{"文字稿", func(it *services.LibraryItem) { it.Transcript = "字" }, func(d *contentDerived) bool { return d.caps.ShowTranscriptTab && d.caps.Transcript == "字" }},
		{"来源", func(it *services.LibraryItem) { it.SourceURI = "https://mp.weixin.qq.com/s/x" }, func(d *contentDerived) bool { return d.caps.IsWechat && d.source.wechat }},
		{"类型", func(it *services.LibraryItem) { it.ItemType = services.LibraryTypeAudio }, func(d *contentDerived) bool { return d.caps.IsMediaItem }},
		{"删除时间", func(it *services.LibraryItem) { it.DeletedAt = 5 }, func(d *contentDerived) bool { return d.caps.Trashed }},
	} {
		s = Screen{}
		s.derivedOf(base)
		it := base
		c.edit(&it)
		d := s.derivedOf(it)
		if computes() != 2 {
			t.Errorf("%s变了应重算一次（共 2 次），实际 %d 次", c.name, computes())
		}
		if !c.check(d) {
			t.Errorf("%s变了之后派生值没有跟着变：%+v", c.name, d)
		}
	}
}

// 三个派生值函数的调用只能在 detailderived.go 里：别处直接调用就绕过了缓存，每帧重算（读数见 TestDetailReaderFrameBudget）。
func TestDerivedValuesAreOnlyComputedThroughTheCache(t *testing.T) {
	t.Parallel()
	call := regexp.MustCompile(`\b(deriveCaps|wordCount|sourceViewOf)\(`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "detailderived.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "func ") {
				continue // 注释与函数定义
			}
			if m := call.FindString(trimmed); m != "" {
				t.Errorf("%s:%d 直接调用了 %s：要经 Screen.derivedOf 的缓存", name, i+1, m)
			}
		}
	}
}

// 画面上的详情：帧与帧之间输入不变就不重算；打字改标题、换一条才重算。
func TestDetailFramesReuseTheDerivedValues(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	r.pick(detailNoteID)
	r.frames(10)
	n := r.screen.detail.derived.computes
	if n < 1 {
		t.Fatalf("详情画出来之后应算过派生值，实际 %d 次", n)
	}
	r.frames(30)
	if got := r.screen.detail.derived.computes; got != n {
		t.Fatalf("内容不变的 30 帧里不该再算：%d → %d 次", n, got)
	}
	r.editTitle("x")
	r.frames(3)
	if got := r.screen.detail.derived.computes; got != n {
		t.Fatalf("敲标题不改正文，不该重算：%d → %d 次", n, got)
	}
	r.pick(detailForumID)
	r.frames(3)
	if got := r.screen.detail.derived.computes; got <= n {
		t.Fatalf("换到另一条（正文不同）应重算：%d → %d 次", n, got)
	}
}
