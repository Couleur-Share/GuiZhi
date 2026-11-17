package island

import "testing"

// fakeIsland 是登记用的假岛：Close 幂等（与 *Island 一致），只计「真正销毁」的次数。
type fakeIsland struct {
	closed *int
	done   bool
}

func (f *fakeIsland) Close() {
	if !f.done {
		f.done = true
		*f.closed++
	}
}

// 收托盘时 CloseAll 销毁登记里的全部岛，且每个只销毁一次；登记随即清空。
func TestGroupCloseAllClosesEachOnce(t *testing.T) {
	var g Group
	var a, b int
	ia, ib := &fakeIsland{closed: &a}, &fakeIsland{closed: &b}
	g.Track(ia)
	g.Track(ib)
	g.Track(ib) // 重复登记不重复销毁
	if g.Len() != 2 {
		t.Fatalf("登记数 = %d，期望 2", g.Len())
	}
	if n := g.CloseAll(); n != 2 {
		t.Errorf("CloseAll 返回 %d，期望 2", n)
	}
	if a != 1 || b != 1 {
		t.Errorf("销毁次数 a=%d b=%d，期望各 1", a, b)
	}
	if g.Len() != 0 || g.CloseAll() != 0 {
		t.Errorf("CloseAll 之后登记应为空")
	}
}

// 界面自己关掉的岛先 Forget，CloseAll 不再碰它；界面先 Close 过的岛即使仍在登记里，再销毁也是空操作。
func TestGroupForgetAndIdempotentClose(t *testing.T) {
	var g Group
	var gone, kept int
	forgotten, still := &fakeIsland{closed: &gone}, &fakeIsland{closed: &kept}
	g.Track(forgotten)
	g.Track(still)
	forgotten.Close()
	g.Forget(forgotten)
	if g.Len() != 1 { // 假岛的 Close 幂等，光看销毁次数分不出「Forget 生效」与「Forget 什么也没做」，所以直接看登记表
		t.Fatalf("Forget 之后登记数 = %d，期望 1（只剩没通知的那个）", g.Len())
	}
	still.Close() // 界面关了但没通知
	g.CloseAll()
	if gone != 1 {
		t.Errorf("已 Forget 的岛销毁 %d 次，期望 1（只有界面自己那次）", gone)
	}
	if kept != 1 {
		t.Errorf("先 Close 过的岛销毁 %d 次，期望 1（Close 幂等）", kept)
	}
}

// 零值可用；nil 不登记；真正的 *Island 满足 Closer（编译期断言）。
func TestGroupIgnoresNilAndZeroValueIsUsable(t *testing.T) {
	var g Group
	g.Track(nil)
	if g.Len() != 0 || g.CloseAll() != 0 {
		t.Errorf("nil 不应被登记")
	}
}

var _ Closer = (*Island)(nil)
