package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"guizhi-mygo-shell/native/settings"
)

// settingsEnv 是一套隔离的用户数据：真实 SQLite 库 + 独立的 config 目录，不碰用户的数据目录。
type settingsEnv struct {
	lib   *Library
	paths Paths
	store *uiPrefsStore
	ns    *nativeSettings
}

func newSettingsEnv(t *testing.T) *settingsEnv {
	t.Helper()
	ctx := context.Background()
	paths := Paths{UserData: t.TempDir()}
	if err := os.MkdirAll(paths.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lease, err := initDatabaseFile(ctx, paths.DatabasePath(), dbInitOptions{BackupsDir: paths.BackupsDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.release)
	db, err := sqlite.Open(ctx, paths.DatabasePath(), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// 网络代理写入后即时生效（改进程环境变量）：测试结束还原，免得影响同包的其他测试
	t.Cleanup(func() { activeNetProxy.Apply(nil) })

	lib := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	store := openUIPrefsStore(filepath.Join(paths.ConfigDir(), uiPrefsFileName), t.Logf)
	return &settingsEnv{lib: lib, paths: paths, store: store, ns: newNativeSettings(lib, store, t.Logf)}
}

func (e *settingsEnv) prefsFile() string { return filepath.Join(e.paths.ConfigDir(), uiPrefsFileName) }

func (e *settingsEnv) putRow(t *testing.T, key, value string) {
	t.Helper()
	if _, err := e.lib.db.Execute(context.Background(), "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value); err != nil {
		t.Fatal(err)
	}
}

func (e *settingsEnv) row(key string) (string, bool) {
	return e.lib.readRawSetting(context.Background(), key)
}

func (e *settingsEnv) snapshot(t *testing.T) settings.Values {
	t.Helper()
	v, err := e.ns.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (e *settingsEnv) hasPrefsFile() bool {
	_, err := os.Stat(e.prefsFile())
	return err == nil
}

// recorder 记下订阅者收到的每一份设置。
type recorder struct {
	mu  sync.Mutex
	got []settings.Values
}

func (r *recorder) on(v settings.Values) {
	r.mu.Lock()
	r.got = append(r.got, v)
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func TestAppSettingKeysAreWhitelisted(t *testing.T) {
	if len(appSettingKeys) == 0 {
		t.Fatal("appSettingKeys 不该为空")
	}
	for key := range appSettingKeys {
		if !persistedSettingsKeys[key] {
			t.Errorf("settings.App 的键 %q 不在 persistedSettingsKeys 白名单里：settingsSet 会拒绝写入", key)
		}
		if aiSettingsKeys[key] {
			t.Errorf("%q 是 AI 连接配置：它在 ai-models.json，不归 settings 表", key)
		}
	}
}

func TestNativeSettingsSnapshotDefaults(t *testing.T) {
	env := newSettingsEnv(t)
	got := env.snapshot(t)
	if !got.Equal(settings.Default()) {
		// 代理与 FlareSolverr 的默认值在 Go 与 native/settings 各有一份：这里守住两边一致
		t.Fatalf("全新安装的快照应等于出厂默认值：\n得到 %+v\n期望 %+v", got, settings.Default())
	}
	if env.hasPrefsFile() {
		t.Fatal("读取不该创建 ui-prefs.json")
	}
}

// App 设置直接读表里的原始行：值不是 JSON 的按字符串处理、类型不对的取默认值、范围外的夹取，
// 与表里并存的 AI 与 Electron 时代的键（以及不属于 App 的键）一概不读。
func TestNativeSettingsSnapshotReadsRows(t *testing.T) {
	env := newSettingsEnv(t)
	for key, value := range map[string]string{
		"language":            "en", // 纯文本（旧版直接写的字符串，不是 JSON）
		"backupIntervalHours": "500",
		"backupKeepCount":     `"x"`,
		"enableNotifications": "false",
		"networkProxy":        `{"mode":"bogus","protocol":"ftp","host":"proxy.example","port":99999,"username":"u","password":"p"}`,
		"flareSolverr":        `{"enabled":"yes","connection":"ssh","port":0,"sshHost":"bad host"}`,
		"theme":               `"dark"`, // Electron 时代的界面键：界面设置在 ui-prefs.json，不读它
		"master_password":     `"hunter2"`,
	} {
		env.putRow(t, key, value)
	}
	got := env.snapshot(t)
	app := got.App
	if app.Language != settings.LangEn {
		t.Errorf("language = %q", app.Language)
	}
	if app.BackupIntervalHours != settings.MaxBackupIntervalHours {
		t.Errorf("backupIntervalHours = %d，应夹取到 %d", app.BackupIntervalHours, settings.MaxBackupIntervalHours)
	}
	if app.BackupKeepCount != settings.DefaultBackupKeepCount {
		t.Errorf("backupKeepCount = %d，类型不对应取默认值 %d", app.BackupKeepCount, settings.DefaultBackupKeepCount)
	}
	if app.EnableNotifications {
		t.Error("enableNotifications 应为 false")
	}
	wantProxy := settings.NetworkProxy{Mode: "system", Protocol: "http", Host: "proxy.example", Port: 7890, Username: "u", Password: "p", Bypass: settings.DefaultProxy().Bypass}
	if app.NetworkProxy != wantProxy {
		t.Errorf("networkProxy 应按 Go 侧规则归一：\n得到 %v\n期望 %v", app.NetworkProxy, wantProxy)
	}
	wantFlare := settings.FlareSolverr{Enabled: false, Connection: "ssh", Port: 8191}
	if app.FlareSolverr != wantFlare {
		t.Errorf("flareSolverr = %+v，期望 %+v", app.FlareSolverr, wantFlare)
	}
	if !got.UI.Equal(settings.DefaultUI()) {
		t.Error("表里的 theme 不属于 App，界面设置不该受它影响")
	}
}

func TestNativeSettingsUpdateWritesBothStores(t *testing.T) {
	env := newSettingsEnv(t)
	var rec recorder
	cancel := env.ns.Subscribe(rec.on)
	defer cancel()

	next, err := env.ns.Update(func(v *settings.Values) {
		v.UI.ThemeMode, v.UI.FontSize, v.UI.ThemeHue = settings.ThemeDark, settings.FontLarge, 999 // 色相会被夹到 360
		v.App.Language, v.App.BackupIntervalHours, v.App.BackupKeepCount = settings.LangEn, 6, 9999
		v.App.NetworkProxy.Mode = "direct"
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.UI.ThemeHue != 360 || next.App.BackupKeepCount != settings.MaxBackupKeepCount {
		t.Errorf("返回值应是归一后的值：色相 %v，保留份数 %d", next.UI.ThemeHue, next.App.BackupKeepCount)
	}
	if got := env.snapshot(t); !got.Equal(next) {
		t.Fatalf("Update 的返回值应与随后读到的快照一致：\n%+v\n%+v", next, got)
	}

	// 界面设置只进 ui-prefs.json；Go 侧设置只进表
	top := readPrefsFile(t, env.prefsFile())
	var inFile map[string]json.RawMessage
	if err := json.Unmarshal(top["settings"], &inFile); err != nil {
		t.Fatal(err)
	}
	if string(inFile["themeMode"]) != `"dark"` || string(inFile["fontSize"]) != `"large"` {
		t.Errorf("界面设置没写进文件：%v", inFile)
	}
	for _, appKey := range []string{"language", "backupIntervalHours", "backupKeepCount", "networkProxy"} {
		if _, leaked := inFile[appKey]; leaked {
			t.Errorf("Go 侧设置 %q 不该写进 ui-prefs.json（备份还原时会与库脱节）", appKey)
		}
	}
	for key, want := range map[string]string{"language": `"en"`, "backupIntervalHours": "6", "backupKeepCount": "100"} {
		if got, ok := env.row(key); !ok || got != want {
			t.Errorf("表里的 %s = %q（存在=%v），期望 %q", key, got, ok, want)
		}
	}
	if got, _ := env.row("networkProxy"); !strings.Contains(got, `"mode":"direct"`) {
		t.Errorf("networkProxy 行 = %s", got)
	}
	for _, uiKey := range []string{"themeMode", "fontSize", "themeHue"} {
		if _, leaked := env.row(uiKey); leaked {
			t.Errorf("界面设置 %q 不该写进 settings 表", uiKey)
		}
	}
	if rec.count() != 1 {
		t.Fatalf("订阅者应恰好被通知一次，实际 %d 次", rec.count())
	}
	if !rec.got[0].Equal(next) {
		t.Error("通知里的设置应与返回值一致")
	}

	// 没有变化：不写盘、不通知
	before, err := os.Stat(env.prefsFile())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := env.ns.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark }); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(env.prefsFile()); !after.ModTime().Equal(before.ModTime()) {
		t.Error("没有变化的 Update 不该再写文件")
	}
	if rec.count() != 1 {
		t.Errorf("没有变化的 Update 不该通知：%d 次", rec.count())
	}
}

// 校验在任何写入之前完成：被拒绝的更新不留下半截状态，界面设置文件也不会先被改掉。
func TestNativeSettingsUpdateRejectsBeforeAnyWrite(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "yt-dlp.exe")
	unpicked := filepath.Join(t.TempDir(), "evil.exe")
	rememberPickedBinaryPath(picked)

	cases := []struct {
		name    string
		mutate  func(*settings.Values)
		wantErr string
		dbKey   string
	}{
		{"SSH 模式缺主机别名", func(v *settings.Values) {
			v.App.FlareSolverr = settings.FlareSolverr{Enabled: true, Connection: "ssh", Port: 8191}
		}, "SSH", "flareSolverr"},
		{"yt-dlp 路径不是选择器返回过的", func(v *settings.Values) { v.App.YtDlpPath = unpicked }, "ytDlpPath", "ytDlpPath"},
		{"ffmpeg 路径不是选择器返回过的", func(v *settings.Values) { v.App.FfmpegPath = unpicked }, "ffmpegPath", "ffmpegPath"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newSettingsEnv(t)
			var rec recorder
			env.ns.Subscribe(rec.on)
			_, err := env.ns.Update(func(v *settings.Values) {
				v.UI.ThemeMode = settings.ThemeDark // 同一次更新里还有合法的界面改动
				c.mutate(v)
			})
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("应被拒绝并说明原因（含 %q）：%v", c.wantErr, err)
			}
			if env.hasPrefsFile() || env.store.UI().ThemeMode != settings.ThemeSystem {
				t.Error("被拒绝的更新不该改动界面设置文件")
			}
			if _, ok := env.row(c.dbKey); ok {
				t.Errorf("被拒绝的更新不该写 %s", c.dbKey)
			}
			if rec.count() != 0 {
				t.Error("被拒绝的更新不该通知订阅者")
			}
			if got := env.snapshot(t); !got.Equal(settings.Default()) {
				t.Error("快照应保持不变")
			}
		})
	}

	t.Run("选择器返回过的路径可写入，空串清除", func(t *testing.T) {
		env := newSettingsEnv(t)
		if _, err := env.ns.Update(func(v *settings.Values) { v.App.YtDlpPath = picked }); err != nil {
			t.Fatal(err)
		}
		if got := env.snapshot(t).App.YtDlpPath; got != picked {
			t.Fatalf("ytDlpPath = %q", got)
		}
		if _, err := env.ns.Update(func(v *settings.Values) { v.App.YtDlpPath = "" }); err != nil {
			t.Fatalf("空串表示清除，应被接受：%v", err)
		}
		if got := env.snapshot(t).App.YtDlpPath; got != "" {
			t.Fatalf("清除后 ytDlpPath = %q", got)
		}
	})
}

// 表写入失败时，已经写出的界面设置文件要恢复原样（文件先写、表后写，不能留下一半）。
func TestNativeSettingsUpdateRollsBackUIFileWhenDatabaseFails(t *testing.T) {
	failLanguage := func(t *testing.T, env *settingsEnv) {
		t.Helper()
		if _, err := env.lib.db.Execute(context.Background(),
			"CREATE TRIGGER fail_language BEFORE INSERT ON settings WHEN NEW.key = 'language' BEGIN SELECT RAISE(ABORT, 'boom-language'); END"); err != nil {
			t.Fatal(err)
		}
	}
	mutate := func(v *settings.Values) {
		v.UI.ThemeMode = settings.ThemeDark
		v.App.Language = settings.LangEn
	}

	t.Run("文件原本不存在：恢复成没有文件", func(t *testing.T) {
		env := newSettingsEnv(t)
		failLanguage(t, env)
		var rec recorder
		env.ns.Subscribe(rec.on)
		_, err := env.ns.Update(mutate)
		if err == nil || !strings.Contains(err.Error(), "boom-language") {
			t.Fatalf("应返回数据库的错误原因：%v", err)
		}
		if env.hasPrefsFile() || env.store.Initialized() || env.store.UI().ThemeMode != settings.ThemeSystem {
			t.Error("数据库写入失败后界面设置应恢复成没有文件")
		}
		if rec.count() != 0 {
			t.Error("失败的更新不该通知")
		}
	})

	t.Run("文件原本存在：只恢复被改的键", func(t *testing.T) {
		env := newSettingsEnv(t)
		if _, err := env.ns.Update(func(v *settings.Values) { v.UI.FontSize = settings.FontSmall }); err != nil {
			t.Fatal(err)
		}
		if err := env.store.Set("keep.pref", "x"); err != nil {
			t.Fatal(err)
		}
		failLanguage(t, env)
		if _, err := env.ns.Update(mutate); err == nil {
			t.Fatal("应失败")
		}
		reopened := openUIPrefsStore(env.prefsFile(), nil)
		ui := reopened.UI()
		if ui.ThemeMode != settings.ThemeSystem || ui.FontSize != settings.FontSmall {
			t.Errorf("应只撤销本次的改动：主题 %s、字号 %s", ui.ThemeMode, ui.FontSize)
		}
		if _, ok := reopened.Get("keep.pref"); !ok {
			t.Error("无关的偏好不该受影响")
		}
	})
}

func TestNativeSettingsSubscribe(t *testing.T) {
	env := newSettingsEnv(t)
	var a, b recorder
	cancelA := env.ns.Subscribe(a.on)
	env.ns.Subscribe(b.on)
	if _, err := env.ns.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark }); err != nil {
		t.Fatal(err)
	}
	cancelA()
	cancelA() // 可重复取消
	if _, err := env.ns.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeLight }); err != nil {
		t.Fatal(err)
	}
	if a.count() != 1 || b.count() != 2 {
		t.Fatalf("通知次数 a=%d b=%d，期望 1 和 2", a.count(), b.count())
	}
}

// 订阅者回调里再读写设置不能死锁（通知在所有锁之外进行）。
func TestNativeSettingsSubscriberMayReenter(t *testing.T) {
	env := newSettingsEnv(t)
	var entered atomic.Bool // 不能用 sync.Once：回调里的 Update 会再次通知到这里，Once 会等自己
	var inner error
	env.ns.Subscribe(func(settings.Values) {
		if !entered.CompareAndSwap(false, true) {
			return
		}
		if _, err := env.ns.Snapshot(); err != nil {
			inner = err
			return
		}
		_, inner = env.ns.Update(func(v *settings.Values) { v.UI.FontSize = settings.FontLarge })
	})
	done := make(chan error, 1)
	go func() {
		_, err := env.ns.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark })
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil || inner != nil {
			t.Fatalf("外层 %v，回调里 %v", err, inner)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("订阅者里读写设置导致死锁")
	}
	if got := env.snapshot(t).UI; got.ThemeMode != settings.ThemeDark || got.FontSize != settings.FontLarge {
		t.Errorf("两次更新都应生效：%s %s", got.ThemeMode, got.FontSize)
	}
}

// 并发更新是串行的「读—改—写」：各改各的键，一个都不能丢。
func TestNativeSettingsConcurrentUpdatesKeepEveryChange(t *testing.T) {
	env := newSettingsEnv(t)
	var wg sync.WaitGroup
	for _, scenario := range settings.AIScenarios {
		wg.Add(1)
		go func(scenario string) {
			defer wg.Done()
			if _, err := env.ns.Update(func(v *settings.Values) {
				v.UI.ScenarioModelDefaults[scenario] = "model-" + scenario
			}); err != nil {
				t.Error(err)
			}
		}(scenario)
	}
	wg.Wait()
	got := env.snapshot(t).UI.ScenarioModelDefaults
	for _, scenario := range settings.AIScenarios {
		if got[scenario] != "model-"+scenario {
			t.Errorf("场景 %s 的更新丢了：%v", scenario, got)
		}
	}
	if reopened := openUIPrefsStore(env.prefsFile(), nil).UI().ScenarioModelDefaults; len(reopened) != len(settings.AIScenarios) {
		t.Errorf("落盘的内容也应完整：%v", reopened)
	}
}
