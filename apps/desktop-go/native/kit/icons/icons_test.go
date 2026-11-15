package icons

import "testing"

// 全部图标都必须能被 MyGo 解析：生成脚本的输出格式一旦偏离，这里先红。
func TestAllIconsParse(t *testing.T) {
	names := Names()
	if len(names) < 100 {
		t.Fatalf("图标数量异常：%d", len(names))
	}
	for _, n := range names {
		if Get(n) == nil {
			t.Errorf("图标 %q 无法解析", n)
		}
	}
}

func TestUnknownIconIsNil(t *testing.T) {
	if Get("NoSuchIcon") != nil {
		t.Fatal("未知图标应返回 nil")
	}
}
