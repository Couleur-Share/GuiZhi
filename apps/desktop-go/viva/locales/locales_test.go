package locales

import "testing"

func TestLanguageSwitch(t *testing.T) {
	t.Cleanup(func() { SetLang(Zh) })

	SetLang(Zh)
	if got := SettingsBackgroundTasks(); got != "后台任务" {
		t.Errorf("中文：%q", got)
	}
	SetLang(En)
	if got := SettingsBackgroundTasks(); got != "Background tasks" {
		t.Errorf("英文：%q", got)
	}
}

func TestInterpolation(t *testing.T) {
	t.Cleanup(func() { SetLang(Zh) })

	SetLang(Zh)
	if got := ThemedReadingActionFailed("保存"); got != "保存失败" {
		t.Errorf("中文插值：%q", got)
	}
	SetLang(En)
	if got := ThemedReadingActionFailed("Save"); got != "Save failed" {
		t.Errorf("英文插值：%q", got)
	}
	// 数字等非字符串参数按 fmt.Sprint 展开
	SetLang(Zh)
	if got := ThemedReadingImageCount(3); got != "3 张" {
		t.Errorf("数字插值：%q", got)
	}
}

func TestParseLang(t *testing.T) {
	cases := map[string]Lang{"zh": Zh, "zh-Hans": Zh, "zh-CN": Zh, "en": En, "en-US": En, "EN": En, "": Zh, "fr": Zh}
	for tag, want := range cases {
		if got := ParseLang(tag); got != want {
			t.Errorf("ParseLang(%q) = %v，期望 %v", tag, got, want)
		}
	}
	if Zh.Tag() != "zh" || En.Tag() != "en" {
		t.Errorf("Tag 应与渲染层 language 设置的取值一致")
	}
}
