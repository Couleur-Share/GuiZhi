package main

// 对应 services/media/funasr-paths.ts：本地转写引擎的目录布局、安装判定与安装状态文件。
//
// 两套落盘形态共用同一 root（tools/funasr/）：
//   - Windows：Python 运行时 + venv（SenseVoice + cam++，含说话人分离）
//   - macOS arm64：llama.cpp / GGUF 预编译包。Go 壳只保留「识别已装」的判定，不负责安装与拉起
//     （平台政策：官方仅支持 Windows x64），见 MYGO-MIGRATION.md。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	funasrModelName     = "sensevoice"
	funasrPortDefault   = 8620
	ggufSensevoiceFile  = "sensevoice-small-q8.gguf"
	ggufVadFile         = "fsmn-vad.gguf"
	ggufCliName         = "llama-funasr-sensevoice"
	funasrFlavorPython  = "python"
	funasrFlavorGguf    = "gguf"
	funasrStateFileName = "state.json"
)

type funasrPaths struct {
	Root          string
	PythonExe     string
	VenvDir       string
	VenvPython    string
	ServerScript  string
	FunasrPackage string
	ModelsDir     string
	GgufRuntime   string
	GgufModels    string
	SensevoiceGGU string
	VadGguf       string
	SensevoiceCli string
	StateFile     string
}

func (l *Library) funasrRoot() string { return filepath.Join(l.paths.ToolsDir(), "funasr") }

func (l *Library) funasrPaths() funasrPaths { return funasrPathsFor(l.funasrRoot()) }

func funasrPathsFor(root string) funasrPaths {
	ggufRuntime, ggufModels := filepath.Join(root, "gguf-runtime"), filepath.Join(root, "gguf")
	return funasrPaths{
		Root:          root,
		PythonExe:     filepath.Join(root, "python", "python.exe"),
		VenvDir:       filepath.Join(root, "env"),
		VenvPython:    filepath.Join(root, "env", "Scripts", "python.exe"),
		ServerScript:  filepath.Join(root, "server.py"),
		FunasrPackage: filepath.Join(root, "env", "Lib", "site-packages", "funasr", "__init__.py"),
		ModelsDir:     filepath.Join(root, "models"),
		GgufRuntime:   ggufRuntime,
		GgufModels:    ggufModels,
		SensevoiceGGU: filepath.Join(ggufModels, ggufSensevoiceFile),
		VadGguf:       filepath.Join(ggufModels, ggufVadFile),
		SensevoiceCli: filepath.Join(ggufRuntime, ggufCliName),
		StateFile:     filepath.Join(root, funasrStateFileName),
	}
}

// isFunasrPythonInstalled：Windows Python 引擎是否装好（含说话人分离）。
func isFunasrPythonInstalled(p funasrPaths) bool {
	return fileExists(p.FunasrPackage) && fileExists(p.VenvPython)
}

// findGgufSensevoiceCli 在 runtime 目录里找 CLI：官方 tar 可能平铺，也可能带一层目录；深度受限避免扫到无关树。
func findGgufSensevoiceCli(runtimeDir string, maxDepth int) string {
	var walk func(dir string, depth int) string
	walk = func(dir string, depth int) string {
		if depth < 0 || !fileExists(dir) {
			return ""
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			if e.Type().IsRegular() && e.Name() == ggufCliName {
				return filepath.Join(dir, e.Name())
			}
		}
		for _, e := range entries {
			if e.IsDir() {
				if found := walk(filepath.Join(dir, e.Name()), depth-1); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(runtimeDir, maxDepth)
}

// isFunasrGgufInstalled：macOS arm64 GGUF 引擎是否装好。
func isFunasrGgufInstalled(p funasrPaths) bool {
	cli := ""
	if st, err := os.Stat(p.SensevoiceCli); err == nil && st.Mode().IsRegular() {
		cli = p.SensevoiceCli
	} else {
		cli = findGgufSensevoiceCli(p.GgufRuntime, 3)
	}
	return cli != "" && fileExists(p.SensevoiceGGU) && fileExists(p.VadGguf)
}

// isFunasrInstalled：任一形态装好即视为已安装。服务脚本不计入：它由应用在每次启动前落盘，缺失可自愈。
func isFunasrInstalled(p funasrPaths) bool {
	return isFunasrPythonInstalled(p) || isFunasrGgufInstalled(p)
}

// resolveFunasrEngineFlavor：当前实际可用的引擎形态；两套都在时 Python 优先（功能更全）。
func resolveFunasrEngineFlavor(p funasrPaths) string {
	switch {
	case isFunasrPythonInstalled(p):
		return funasrFlavorPython
	case isFunasrGgufInstalled(p):
		return funasrFlavorGguf
	}
	return ""
}

// resolveGgufSensevoiceCli：GGUF CLI 的绝对路径（安装后可能不在 gguf-runtime 根目录）。
func resolveGgufSensevoiceCli(p funasrPaths) string {
	if fileExists(p.SensevoiceCli) {
		return p.SensevoiceCli
	}
	return findGgufSensevoiceCli(p.GgufRuntime, 3)
}

// funasrInstallState 对应 state.json。字段顺序、省略规则与 TS 的 JSON.stringify 一致。
type funasrInstallState struct {
	FunasrVersion string `json:"funasrVersion,omitempty"`
	PythonVersion string `json:"pythonVersion,omitempty"`
	// 安装形态：缺省按路径探测（兼容旧 state.json）
	Flavor      string `json:"flavor,omitempty"`
	InstalledAt string `json:"installedAt"`
}

func readFunasrState(p funasrPaths) *funasrInstallState {
	raw, err := os.ReadFile(p.StateFile)
	if err != nil {
		return nil
	}
	var st funasrInstallState
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil {
		return nil
	}
	if _, ok := probe["installedAt"]; !ok {
		return nil
	}
	if err := json.Unmarshal(raw, &st); err != nil { // installedAt 不是字符串
		return nil
	}
	return &st
}

func writeFunasrState(st funasrInstallState, p funasrPaths) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.StateFile, append(b, '\n'), 0o644)
}
