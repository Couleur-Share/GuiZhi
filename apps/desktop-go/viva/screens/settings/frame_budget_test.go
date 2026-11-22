package settings_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// budgetData 是「数据与备份」页的帧耗时场景：40 份旧快照加 30 份完整快照的整页，
// 可选地再叠一个配置迁移对话框（对话框是浮层，要单独量一次它的帧成本）。
// 页面里五块（计划、完整备份仓库、旧快照、Markdown 导出、配置迁移）都在，滚动与悬停都落在真实的长列表上。
func budgetData(t *testing.T, ready bool, dialog string) *rig {
	t.Helper()
	e := newDataEnv()
	var legacy []services.BackupInfo
	for i := 0; i < 40; i++ {
		kind := services.BackupKindManual
		if i%3 == 0 {
			kind = services.BackupKindAuto
		}
		legacy = append(legacy, legacyBackup(fmt.Sprintf("knowledge-%s-%03d.db", kind, i), kind, int64(300+i*40_000)))
	}
	e.backup.SetLegacy(legacy)
	e.backup.SetStatus(services.RepositoryStatus{Initialized: ready, AutomaticAccessAvailable: ready})
	if ready {
		var snaps []services.BackupInfo
		for i := 0; i < 30; i++ {
			snaps = append(snaps, repoSnap(fmt.Sprintf("snap-%03d", i), 10+i))
		}
		e.backup.SetSnapshots(snaps)
	}
	e.cfg.SetRead(encryptedRead())
	r := e.open(t)
	waitUntil(r, "旧快照列表出现", func() bool { return r.hasText("knowledge-manual-001.db") })
	if ready {
		waitUntil(r, "完整快照列表出现", func() bool {
			return r.hasText(locales.SettingsFullBackupSnapshotSummary(10, 5, "4.0 MB", 33))
		})
	} else {
		waitUntil(r, "启用表单出现", func() bool { return r.has(gfx.RoleButton, locales.SettingsFullBackupEnable()) })
	}
	switch dialog {
	case "export":
		openExportDialog(r)
	case "import":
		pickImportFile(r)
		waitUntil(r, "导入预览对话框", func() bool { return r.dialogOpen(locales.SettingsConfigImportTitle()) })
	}
	return r
}

// TestFrameBudget 读出设置页的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
// 口径与 viva/screens/imports 的同名测试相同：Frame() 只记录不栅格化，量的是界面线程的出帧成本；
// 栅格化加回读另量一次（Image()）作为上界。默认跳过：VIVA_FRAME_BUDGET=1 时运行，结果记进开发文档 §8.4。
//
// 页面：通用、外观（M3-2a Step 1），以及数据与备份的四个场景（M3-2a Step 3 收尾）：
// 已启用完整备份的整页（含旧快照列表、完整快照列表、配置迁移）、未启用（启用表单加换机导入）的整页、
// 导出对话框打开、导入预览对话框打开。对话框打开时滚轮落在遮罩上，页面不动，所以不量滚动。
// M3-2b 追加：采集与转写（capture）、MCP 接入（mcp）、AI 模型服务工作台（ai_workbench）、快速配置向导的服务步骤（ai_wizard）。
func TestFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	pages := []struct {
		name     string
		open     func(t *testing.T) *rig
		noScroll bool
	}{
		{"general", func(t *testing.T) *rig { return openGeneral(t, newStore()) }, false},
		{"appearance", func(t *testing.T) *rig {
			r, _ := openAppearance(t, newStore())
			return r
		}, false},
		{"data_repo_ready", func(t *testing.T) *rig { return budgetData(t, true, "") }, false},
		{"data_repo_setup", func(t *testing.T) *rig { return budgetData(t, false, "") }, false},
		{"data_export_dialog", func(t *testing.T) *rig { return budgetData(t, true, "export") }, true},
		{"data_import_dialog", func(t *testing.T) *rig { return budgetData(t, true, "import") }, true},
		// M3-2b：采集与转写、MCP 接入、AI 模型服务（工作台与快速配置向导）。向导是对话框，滚轮落在遮罩上，不量滚动。
		{"capture", func(t *testing.T) *rig {
			r := newCaptureEnv().open(t)
			r.frames(10)
			return r
		}, false},
		{"mcp", func(t *testing.T) *rig {
			r := newMCPEnv().open(t)
			r.frames(10)
			return r
		}, false},
		{"ai_workbench", func(t *testing.T) *rig {
			r := newAIEnv().open(t)
			waitText(r, locales.SettingsAiWorkbenchStatusOverview())
			return r
		}, false},
		{"ai_wizard", func(t *testing.T) *rig {
			e := newAIEnv()
			e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
			return e.openWizardAtService(t)
		}, true},
	}
	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			heapBefore := frametime.HeapInUse()
			r := pg.open(t)
			t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB", pg.name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			idle, err := frametime.Measure(300, func(int) error { return r.s.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", pg.name, idle)

			points := [][2]float32{{640, 300}, {640, 380}}
			hover, err := frametime.Measure(200, func(i int) error {
				p := points[i%2]
				if err := r.s.Move(p[0], p[1]); err != nil {
					return err
				}
				return r.s.Frame()
			})
			if err != nil {
				t.Fatalf("悬停帧失败：%v", err)
			}
			t.Logf("%s 悬停帧（移动指针+出帧）：%s", pg.name, hover)

			if !pg.noScroll {
				scrollStep := func(dy float32) error {
					if err := r.s.Scroll(gfx.ScrollEvent{X: 640, Y: 450, DeltaY: dy, Precise: true}); err != nil {
						return err
					}
					return r.s.Frame()
				}
				top := frametime.Fingerprint(r.s.Access())
				for i := 0; i < 20; i++ {
					if err := scrollStep(40); err != nil {
						t.Fatalf("预检滚动失败：%v", err)
					}
				}
				if top == frametime.Fingerprint(r.s.Access()) {
					t.Fatalf("向下滚动 800 px 之后画面没有变化，滚动读数无效（页面不够长，或滚动位置不在滚动区里）")
				}
				for i := 0; i < 20; i++ {
					if err := scrollStep(-40); err != nil {
						t.Fatalf("预检回滚失败：%v", err)
					}
				}
				scroll, err := frametime.Measure(200, func(i int) error {
					if (i/20)%2 == 1 {
						return scrollStep(-40)
					}
					return scrollStep(40)
				})
				if err != nil {
					t.Fatalf("滚动帧失败：%v", err)
				}
				t.Logf("%s 滚动一步（滚动+出帧）：%s", pg.name, scroll)
			}

			raster, err := frametime.Measure(30, func(int) error {
				_, err := r.s.Image()
				return err
			})
			if err != nil {
				t.Fatalf("栅格化失败：%v", err)
			}
			t.Logf("%s 栅格化+回读（Image，上界）：%s", pg.name, raster)
		})
	}
}
