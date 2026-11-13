package main

// 灰度发布的更新通道对拍（本地 httptest 假 GitHub，不联网）。
//
// 场景 S1–S6 与「用真实 electron-updater 6.8.9 + Electron 45 跑同一套假源」的实测结果一一对应
// （见 spikes/MYGO-MIGRATION.md「灰度发布结论与更新通道约束」）：Go 壳的更新器必须给出与 Electron 版一致的选择，
// 否则同一批用户在两个壳里会看到不同的更新。
// 另外两条（G1/G2）固定电子构建器产物命名与 CI 上传集合不一致带来的坑。

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type grayRelease struct{ tag string }

func (r grayRelease) pre() bool { return strings.Contains(r.tag, "-") }

// setupGray 按「最新在前」的发布列表配置假源：atom 条目顺序 = 列表顺序；/latest = 第一个非预发布。
func setupGray(f *updaterFixture, tags ...string) {
	f.atomTags = nil
	f.latestTag = ""
	var api []string
	for _, tg := range tags {
		r := grayRelease{tag: tg}
		f.atomTags = append(f.atomTags, tg)
		if !r.pre() && f.latestTag == "" {
			f.latestTag = tg
		}
		api = append(api, fmt.Sprintf(`{"tag_name":%q,"prerelease":%v,"draft":false}`, tg, r.pre()))
	}
	f.apiReleases = "[" + strings.Join(api, ",") + "]"
	// 与 electron-builder 一致：正式版产出 latest.yml，带后缀的预发布版（0.29.0-go.1）产出 go.yml
	f.ymlOK = func(tag, file string) bool {
		r := grayRelease{tag: tag}
		if !r.pre() {
			return file == "latest.yml"
		}
		chans := strings.SplitN(strings.TrimPrefix(tag, "v"), "-", 2)[1]
		return file == strings.SplitN(chans, ".", 2)[0]+".yml"
	}
}

func checkVersion(t *testing.T, f *updaterFixture, channel string) (version string, available bool, errMsg string) {
	t.Helper()
	res := f.u.Check(context.Background(), rawOpts(map[string]any{"channel": channel}))
	if res["success"] != true {
		e, _ := res["error"].(string)
		return "", false, e
	}
	v := res["result"].(map[string]any)["updateInfo"].(map[string]any)["version"].(string)
	return v, res["updateAvailable"] == true, ""
}

func TestUpdaterGrayChannelScenarios(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		channel   string
		releases  []string
		wantVer   string
		wantAvail bool
	}{
		{"S1 稳定通道 Electron 0.28.0，存在 go.1 预发布", "0.28.0", "stable", []string{"v0.29.0-go.1", "v0.28.0"}, "0.28.0", false},
		{"S2 预览通道 Electron 0.28.0，同上", "0.28.0", "preview", []string{"v0.29.0-go.1", "v0.28.0"}, "0.29.0-go.1", true},
		{"S3 灰度 go.1 + 稳定通道，主线发 0.29.0", "0.29.0-go.1", "stable", []string{"v0.29.0", "v0.29.0-go.1", "v0.28.0"}, "0.29.0", true},
		{"S4 灰度 go.1 + 预览通道，主线发 0.29.0", "0.29.0-go.1", "preview", []string{"v0.29.0", "v0.29.0-go.1", "v0.28.0"}, "0.29.0-go.1", false},
		{"S5 灰度 go.1 + 预览通道，出现 go.2", "0.29.0-go.1", "preview", []string{"v0.29.0-go.2", "v0.29.0", "v0.29.0-go.1", "v0.28.0"}, "0.29.0-go.2", true},
		{"S6 预览通道 0.28.0，主线 0.28.1 比 go.1 晚发", "0.28.0", "preview", []string{"v0.28.1", "v0.29.0-go.1", "v0.28.0"}, "0.28.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newUpdaterFixture(t, c.current)
			setupGray(f, c.releases...)
			v, avail, e := checkVersion(t, f, c.channel)
			if e != "" {
				t.Fatalf("检查失败: %s", e)
			}
			if v != c.wantVer || avail != c.wantAvail {
				t.Fatalf("得到 %s（可更新=%v），期望 %s（可更新=%v）", v, avail, c.wantVer, c.wantAvail)
			}
		})
	}
}

// G1：预发布 tag 下只有 latest.yml（没有 go.yml）时，预览通道回退读 latest.yml（与 electron-updater 相同）。
func TestUpdaterGrayPrereleaseFallsBackToLatestYml(t *testing.T) {
	f := newUpdaterFixture(t, "0.28.0")
	setupGray(f, "v0.29.0-go.1", "v0.28.0")
	f.ymlOK = func(tag, file string) bool { return file == "latest.yml" }
	v, avail, e := checkVersion(t, f, "preview")
	if e != "" || v != "0.29.0-go.1" || !avail {
		t.Fatalf("v=%q avail=%v err=%q", v, avail, e)
	}
}

// G2：预发布 tag 下既没有 go.yml 也没有 latest.yml（electron-builder 产出 go.yml，但现有发布工作流只上传 latest*.yml）
// → 预览通道检查失败；稳定通道不受影响。release-go.yml 与 release.yml 都会上传 latest.yml，此情形只会出现在人手建发布且漏传清单时。
func TestUpdaterGrayPrereleaseWithoutManifestFails(t *testing.T) {
	f := newUpdaterFixture(t, "0.28.0")
	setupGray(f, "v0.29.0-go.1", "v0.28.0")
	f.ymlOK = func(tag, file string) bool { return !strings.Contains(tag, "-") && file == "latest.yml" }
	if _, _, e := checkVersion(t, f, "preview"); e == "" {
		t.Fatal("预发布缺清单时预览通道应报错")
	}
	v, avail, e := checkVersion(t, f, "stable")
	if e != "" || v != "0.28.0" || avail {
		t.Fatalf("稳定通道应不受影响: v=%q avail=%v err=%q", v, avail, e)
	}
}

// G3：镜像（generic）路径在预览通道按 semver 取最高并钉 tag，官方源按 atom 发布先后取最新——
// 主线 0.28.1 晚于 go.1 发布时两者不同（S6）。记录下来，灰度期两个来源可能让预览用户看到不同版本。
func TestUpdaterGrayMirrorAndOfficialDiverge(t *testing.T) {
	f := newUpdaterFixture(t, "0.28.0")
	setupGray(f, "v0.28.1", "v0.29.0-go.1", "v0.28.0")
	v, _, e := checkVersion(t, f, "preview")
	if e != "" || v != "0.28.1" {
		t.Fatalf("官方源应得到 0.28.1: %q %q", v, e)
	}
	if f.u.lastFeed.tag != "v0.29.0-go.1" {
		t.Fatalf("镜像应钉 go.1（按 semver 取最高），实际 %+v", f.u.lastFeed)
	}
}

// G4：用 release-go.yml 真实产出的清单形态（安装包名 GuiZhi-Setup-<ver>-x64.exe、files 条目没有 size、
// releaseNotes 是带 \r\n 转义的中文双引号串）走完 预览通道 检查 → 下载 → 校验和。
// 2026-11-13 真实 CI 草稿 v0.29.0-go.1 的 latest.yml 即此形态。
func TestUpdaterGrayRealCIManifestShape(t *testing.T) {
	f := newUpdaterFixture(t, "0.28.0")
	setupGray(f, "v0.29.0-go.1", "v0.28.0")
	f.ymlOK = func(tag, file string) bool { return file == "latest.yml" }
	f.manifest = func(tag string) string {
		sum := sha512b64(f.installer)
		return "version: 0.29.0-go.1\nfiles:\n  - url: GuiZhi-Setup-0.29.0-go.1-x64.exe\n    sha512: " + sum + "\n" +
			"path: GuiZhi-Setup-0.29.0-go.1-x64.exe\nsha512: " + sum + "\n" +
			"releaseNotes: \"## v0.29.0-go.1\\r\\n\\r\\n**灰度预览版（Go + WebView2 新外壳）。**\\r\\n\\r\\n- 回退办法：安装 v0.28.0\"\n" +
			"releaseDate: '2026-11-13T03:44:03.877Z'\n"
	}
	ctx := context.Background()
	res := f.u.Check(ctx, rawOpts(map[string]any{"channel": "preview"}))
	if res["success"] != true || res["updateAvailable"] != true {
		t.Fatalf("check = %v", res)
	}
	if got := res["result"].(map[string]any)["updateInfo"].(map[string]any)["version"]; got != "0.29.0-go.1" {
		t.Fatalf("version = %v", got)
	}
	info := f.statuses[len(f.statuses)-1]["info"].(simpleUpdateInfo)
	if !strings.Contains(info.ReleaseNotes, "灰度预览版") || info.ReleaseDate != "2026-11-13T03:44:03.877Z" {
		t.Fatalf("info = %+v", info)
	}
	if r := f.u.Download(ctx, rawOpts(map[string]any{"channel": "preview"})); r["success"] != true {
		t.Fatalf("download = %v", r)
	}
	if sum, _ := fileSHA512(f.u.installerPath); sum != sha512b64(f.installer) {
		t.Fatal("下载文件校验和不一致")
	}
}
