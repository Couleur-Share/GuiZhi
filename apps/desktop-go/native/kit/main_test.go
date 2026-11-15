package kit

import (
	"os"
	"testing"

	"github.com/egoist/mygo/i18n"
)

// TestMain 把界面语言固定为简体中文：断言里的可见文字取自 locales，测试结果不随开发机的系统语言变。
func TestMain(m *testing.M) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	os.Exit(m.Run())
}
