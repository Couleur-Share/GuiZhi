package library

import "time"

// searchDebounce 是顶栏搜索框的防抖：文字停下这么久才进知识库 store（渲染层 TopBar 的 SEARCH_DEBOUNCE_MS）。
const searchDebounce = 300 * time.Millisecond

// SearchTyped 在顶栏搜索框的文字变了之后调用（输入或点清除按钮）。
//
// 渲染层的顶栏把文字存在本地状态里，300 ms 内再变就重新计时，停下之后才调 store 的 setSearchQuery；
// 这里同样每次变化都取消上一次的排期、重排一次。搜索框属于窗口（所有模块共用），文字不经过 Store，
// 所以 Store 里的搜索词只在防抖到点之后才变。
func (s *Screen) SearchTyped(text string) {
	if s.searchStop != nil {
		s.searchStop()
	}
	s.searchStop = s.Store.After(searchDebounce, func() { s.Store.SetSearch(s.Ctx, text) })
}
