package island

import (
	"os"
	"testing"
)

func TestAppendBrowserArgsRestoresUnset(t *testing.T) {
	t.Setenv(browserArgsVar, "x") // 先登记还原点
	os.Unsetenv(browserArgsVar)
	restore := appendBrowserArgs("--a")
	if got := os.Getenv(browserArgsVar); got != "--a" {
		t.Fatalf("追加后 = %q，期望 --a", got)
	}
	restore()
	if v, ok := os.LookupEnv(browserArgsVar); ok {
		t.Fatalf("原本没设的变量应当被取消，实际 %q", v)
	}
}

func TestAppendBrowserArgsKeepsExisting(t *testing.T) {
	t.Setenv(browserArgsVar, "--disable-features=CalculateNativeWinOcclusion  ")
	restore := appendBrowserArgs("--disable-component-update")
	if got := os.Getenv(browserArgsVar); got != "--disable-features=CalculateNativeWinOcclusion --disable-component-update" {
		t.Fatalf("追加后 = %q", got)
	}
	restore()
	if got := os.Getenv(browserArgsVar); got != "--disable-features=CalculateNativeWinOcclusion  " {
		t.Fatalf("还原应当逐字节回到进入前的值，实际 %q", got)
	}
}

func TestAppendBrowserArgsEmptyPreviousIsUnsetLike(t *testing.T) {
	t.Setenv(browserArgsVar, "   ")
	restore := appendBrowserArgs("--a")
	if got := os.Getenv(browserArgsVar); got != "--a" {
		t.Fatalf("空白的旧值不应留下前导空格：%q", got)
	}
	restore()
	if got := os.Getenv(browserArgsVar); got != "   " {
		t.Fatalf("还原 = %q", got)
	}
}
