package main

// 对应 funasr-update.ts + funasr-pip.ts：Windows FunASR 独立更新——保留模型与配置，备份依赖环境，验收失败恢复。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	pypiFunasrURL     = "https://pypi.org/pypi/funasr/json"
	pipIndexURLUpdate = "https://pypi.org/simple"
)

var errInvalidFunasrVersion = errors.New("无效的 FunASR 更新版本")

var reFunasrVersion = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)*$`)

// isNewerFunasrVersion 只接收稳定数字版本，避免预发布、未知格式或版本字符串进入 pip 参数。
func isNewerFunasrVersion(latest, current string) (bool, error) {
	if !reFunasrVersion.MatchString(latest) || !reFunasrVersion.MatchString(current) {
		return false, errors.New("无法识别 FunASR 版本，请重新安装引擎后再检查更新")
	}
	a, b := strings.Split(latest, "."), strings.Split(current, ".")
	for i := 0; i < max(len(a), len(b)); i++ {
		x, y := versionPart(a, i), versionPart(b, i)
		if x != y {
			return x > y, nil
		}
	}
	return false, nil
}

func versionPart(parts []string, i int) float64 {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.ParseFloat(parts[i], 64)
	return n
}

func (e *funasrEngine) pypiURLOr() string { return orString(e.pypiURL, pypiFunasrURL) }

func (e *funasrEngine) resolveLookup() lookupFunc {
	if e.lookup != nil {
		return e.lookup
	}
	return systemLookup
}

func (e *funasrEngine) fetchLatestVersion(ctx context.Context) (string, error) {
	u, err := url.Parse(e.pypiURLOr())
	if err != nil {
		return "", err
	}
	if _, err := resolvePublicAddress(ctx, u.Hostname(), resolveOptions{AllowProxyCompatibilityAddress: activeNetProxy.HasAnyProxyConfigured()}, e.resolveLookup()); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := e.l.toolHTTPClient(true).Do(req) // redirect: error
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("PyPI 检查更新失败：HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", err
	}
	var data struct {
		Info *struct {
			Version string `json:"version"`
			Yanked  any    `json:"yanked"`
		} `json:"info"`
		URLs []struct {
			Yanked any `json:"yanked"`
		} `json:"urls"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	usable := false
	for _, f := range data.URLs {
		if f.Yanked == false {
			usable = true
		}
	}
	if data.Info == nil || data.Info.Version == "" || !reFunasrVersion.MatchString(data.Info.Version) || vTruthyAny(data.Info.Yanked) || !usable {
		return "", errors.New("PyPI 未返回可用的 FunASR 稳定版本")
	}
	return data.Info.Version, nil
}

func (e *funasrEngine) checkUpdate(ctx context.Context) (toolUpdateCheck, error) {
	paths := e.l.funasrPaths()
	if e.platformOr() != "win32" || resolveFunasrEngineFlavor(paths) != funasrFlavorPython {
		return toolUpdateCheck{}, errors.New("当前引擎不支持独立更新；GGUF 运行时需随归知适配更新")
	}
	st := readFunasrState(paths)
	if st == nil || st.FunasrVersion == "" {
		return toolUpdateCheck{}, errors.New("无法读取已安装版本，请重新安装引擎")
	}
	latest, err := e.fetchLatestVersion(ctx)
	if err != nil {
		return toolUpdateCheck{}, err
	}
	newer, err := isNewerFunasrVersion(latest, st.FunasrVersion)
	if err != nil {
		return toolUpdateCheck{}, err
	}
	return toolUpdateCheck{Current: &st.FunasrVersion, Latest: &latest, UpdateAvailable: newer}, nil
}

// ── pip 自检与离线修复（不下载脚本、不改转写依赖）──

// ensurepip --upgrade 会跳过同版本的损坏安装。直接从运行时自带的 wheel 加载完整 pip，
// 强制离线重装自身，避免依赖已经无法启动的 site-packages/pip。
const repairPipScript = `
import ensurepip
from pathlib import Path
import runpy
import sys

wheel = Path(ensurepip.__file__).parent / "_bundled" / ("pip-" + ensurepip.version() + "-py3-none-any.whl")
if not wheel.is_file():
    raise RuntimeError("Bundled pip wheel is missing")
sys.path.insert(0, str(wheel))
sys.argv = ["pip", "--isolated", "--disable-pip-version-check", "install",
            "--no-index", "--no-deps", "--no-cache-dir", "--force-reinstall",
            "--no-warn-script-location", str(wheel)]
runpy.run_module("pip", run_name="__main__", alter_sys=True)
`

func ensureFunasrPip(python string, run funasrToolRunner, onRepair func()) error {
	// --version 不加载安装命令的网络依赖，无法发现 cachecontrol 源码缺失。
	probe := func() error {
		_, err := run(python, []string{"-I", "-m", "pip", "install", "--help"}, funasrToolOptions{})
		return err
	}
	if probe() == nil {
		return nil
	}
	if onRepair != nil {
		onRepair()
	}
	_, err := run(python, []string{"-I", "-c", repairPipScript}, funasrToolOptions{Timeout: 120 * time.Second})
	if err == nil {
		err = probe()
	}
	if err != nil {
		return fmt.Errorf("本地引擎 pip 离线修复失败：%s", jsErrorString(err))
	}
	return nil
}

// stopAndWait 等待进程确实退出；外部托管的服务仍占端口时拒绝覆盖它的依赖。
func (e *funasrEngine) stopAndWait() error {
	e.stop()
	for i := 0; i < 20; i++ {
		time.Sleep(orDuration(e.waitStep, 250*time.Millisecond))
		if !e.isPortListening(1500 * time.Millisecond) {
			return nil
		}
	}
	return errors.New("本地转写服务仍在运行，请关闭外部转写服务后重试")
}

func (e *funasrEngine) diskSpaceFn() diskSpaceFunc {
	if e.diskSpace != nil {
		return e.diskSpace
	}
	return systemDiskSpace
}

// update 独立升级不重装 Python、不重下模型，也不改用户的语音路由。返回（版本，警告）。
func (e *funasrEngine) update(ctx context.Context, version string, onProgress func(funasrInstallProgress)) (string, string, error) {
	if err := e.acquireOperation("已有安装 / 更新 / 卸载任务进行中"); err != nil {
		return "", "", err
	}
	defer e.releaseOperation()
	return e.updateWindows(ctx, version, onProgress)
}

func (e *funasrEngine) updateWindows(ctx context.Context, version string, onProgress func(funasrInstallProgress)) (string, string, error) {
	if !reFunasrVersion.MatchString(version) {
		return "", "", errInvalidFunasrVersion
	}
	// 先于停服务和备份依赖环境：pip 无法经 SOCKS 代理访问索引。
	pipIndex := orString(e.pipIndexURL, pipIndexURLUpdate)
	if funasrSocksBlocked(pipIndex) {
		return "", "", errors.New(funasrSocksProxyHint)
	}
	emit := func(p funasrInstallProgress) {
		if onProgress != nil {
			func() {
				defer func() {
					if recover() != nil {
						fmt.Println("[funasr] 更新进度通知失败，继续维护事务")
					}
				}()
				onProgress(p)
			}()
		}
	}
	// 重新核实远端，避免检查后撤回的版本或渲染进程传入任意包。
	check, err := e.checkUpdate(ctx)
	if err != nil {
		return "", "", err
	}
	if check.Latest == nil || *check.Latest != version {
		return "", "", errors.New("可用版本已变化，请重新检查更新")
	}
	if !check.UpdateAvailable {
		return *check.Current, "", nil
	}
	var result, warning string
	err = e.runMaintenance(func(startService func() error) error {
		v, w, err := e.runUpdateTransaction(version, pipIndex, emit, startService)
		result, warning = v, w
		return err
	})
	if err != nil {
		return "", "", err
	}
	return result, warning, nil
}

func (e *funasrEngine) runUpdateTransaction(version, pipIndex string, emit func(funasrInstallProgress), startService func() error) (string, string, error) {
	paths := e.l.funasrPaths()
	state := readFunasrState(paths)
	if state == nil {
		return "", "", errors.New("引擎安装记录不存在，请重新安装")
	}
	emit(progressPhase("prepare"))
	warnings, err := cleanupFunasrBackups(paths.Root)
	if err != nil {
		return "", "", err
	}
	// 检查发生在停服务、创建备份和改依赖之前，空间不足不影响原引擎。
	budget, err := planFunasrUpdateSpace(e.diskSpaceFn(), paths.Root, paths.VenvDir)
	if err != nil {
		return "", "", err
	}
	envIdentity, err := readDirectoryIdentity(paths.VenvDir)
	if err != nil {
		return "", "", err
	}
	backup, err := createFunasrBackup(paths.Root, *state, version)
	if err != nil {
		return "", "", err
	}
	backupEnv := filepath.Join(backup.Directory, "env")
	temporary := filepath.Join(backup.Directory, "tmp")
	base := e.tool()
	// pip 缓存禁用、临时目录固定，避免更新偷偷占满未检查的系统临时盘。
	runUpdateTool := func(executable string, args []string, opts funasrToolOptions) (string, error) {
		opts.Env = append(os.Environ(), "TMPDIR="+temporary, "TMP="+temporary, "TEMP="+temporary)
		return base(executable, args, opts)
	}

	var (
		changed, stopped, preserveBackup bool
		failure                          error
	)
	cause := func() error {
		hint := fmt.Sprintf("预计备份 %s；更新预留 %s。\n%s", formatFunasrBytes(budget.BackupBytes), formatFunasrBytes(budget.WorkBytes), strings.Join(warnings, "\n"))
		emit(funasrInstallProgress{Phase: "backup", Detail: trimJS(hint)})
		if err := e.stopAndWait(); err != nil {
			return err
		}
		stopped = true
		// venv 不可迁移执行；备份只用于还原到原路径，升级仍在原环境内完成。
		if err := copyTree(paths.VenvDir, backupEnv, true); err != nil {
			return err
		}
		if err := markFunasrBackup(backup, backupReady); err != nil {
			return err
		}
		// 备份期间其他程序也可能占空间；进入写入阶段前再检查一次。
		if err := assertFunasrUpdateSpace(e.diskSpaceFn(), paths.Root, budget.WorkBytes); err != nil {
			return err
		}
		if err := os.Mkdir(temporary, 0o755); err != nil {
			return err
		}
		if err := markFunasrBackup(backup, backupUpdating); err != nil {
			return err
		}
		// 默认保留：只有新版本验收或旧版本恢复成功，才重新允许清理。
		preserveBackup = true
		changed = true
		emit(progressPhase("deps"))
		// 修复也在备份保护范围内；失败仍恢复完整旧环境。
		if err := ensureFunasrPip(paths.VenvPython, runUpdateTool, func() {
			emit(funasrInstallProgress{Phase: "deps", Detail: "正在离线修复更新工具 pip"})
		}); err != nil {
			return err
		}
		var lastEmit time.Time
		if _, err := runUpdateTool(paths.VenvPython, []string{"-I", "-m", "pip", "install", "--no-cache-dir", "--upgrade",
			"--upgrade-strategy", "only-if-needed", "--no-warn-script-location", "--only-binary=:all:",
			"funasr==" + version, "-i", pipIndex}, funasrToolOptions{
			Timeout: 30 * time.Minute,
			OnOutput: func(line string) {
				if time.Since(lastEmit) < 500*time.Millisecond {
					return
				}
				lastEmit = time.Now()
				emit(funasrInstallProgress{Phase: "deps", Detail: utf16Head(line, 80)})
			},
		}); err != nil {
			return err
		}
		emit(progressPhase("verify"))
		if _, err := runUpdateTool(paths.VenvPython, []string{"-I", "-m", "pip", "check"}, funasrToolOptions{}); err != nil {
			return err
		}
		out, err := runUpdateTool(paths.VenvPython, []string{"-I", "-c", "from importlib.metadata import version; print(version('funasr'))"}, funasrToolOptions{})
		if err != nil {
			return err
		}
		if installed := trimJS(out); installed != version {
			return fmt.Errorf("更新后版本不符：%s", installed)
		}
		if err := startService(); err != nil {
			return err
		}
		next := *state
		next.FunasrVersion = version
		if err := writeFunasrState(next, paths); err != nil {
			return err
		}
		preserveBackup = false
		return nil
	}()
	if cause != nil {
		failure = cause
		switch {
		case changed:
			emit(progressPhase("rollback"))
			rollbackErr := func() error {
				if err := e.stopAndWait(); err != nil {
					return err
				}
				if err := preserveFunasrRollbackEnvironment(backup, paths.VenvDir, envIdentity); err != nil {
					return err
				}
				if err := copyTree(backupEnv, paths.VenvDir, true); err != nil {
					return err
				}
				if err := writeFunasrState(*state, paths); err != nil {
					return err
				}
				return startService()
			}()
			if rollbackErr == nil {
				preserveBackup = false
				failure = fmt.Errorf("更新失败，已恢复原版本：%s", jsErrorString(cause))
			} else {
				preserveBackup = true
				// 即使标记失败，磁盘上先前的 updating 也不会被自动清理。
				_ = markFunasrBackup(backup, backupRecoveryRequired)
				failure = fmt.Errorf("更新失败：%s；恢复失败：%s；备份保留在 %s", jsErrorString(cause), jsErrorString(rollbackErr), backup.Directory)
			}
		case stopped:
			// 备份失败或二次空间检查失败时依赖没动过，但先前停掉的服务要恢复。
			if restartErr := startService(); restartErr != nil {
				failure = fmt.Errorf("更新尚未开始：%s；原引擎重新启动失败：%s", jsErrorString(cause), jsErrorString(restartErr))
			}
		}
	}
	if !preserveBackup {
		if w := discardFunasrBackup(backup); w != "" {
			warnings = append(warnings, w)
		}
	}
	warning := strings.Join(warnings, "\n")
	if failure != nil {
		if warning == "" {
			return "", "", failure
		}
		return "", "", fmt.Errorf("%s\n%s", jsErrorString(failure), warning)
	}
	return version, warning, nil
}
