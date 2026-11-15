# 构建 MyGo 壳的 Windows x64 发布目录：<out>\GuiZhi.exe + dist\{renderer,preload} + reading_libraries.json
# 用法（仓库根）：.\apps\desktop-go\scripts\build-shell.ps1 [-Out apps\desktop-go\out\win-x64]
# 产物写入 -Out 指定目录；不要覆盖用户正在运行的 GuiZhi.exe（验证用 pnpm shot，它在隔离副本里构建）。
param([string]$Out = "apps\desktop-go\out\win-x64")
$ErrorActionPreference = "Stop"
$root = (Resolve-Path "$PSScriptRoot\..\..\..").Path
Set-Location $root
$version = (Get-Content apps\desktop\package.json -Raw | ConvertFrom-Json).version
$shell = Join-Path $root "apps\desktop-go"
$dist = Join-Path $shell "dist"

Write-Host "[1/4] 渲染进程与 preload（vite）"
Push-Location apps\desktop
try {
  pnpm exec vite build --config ..\..\apps\desktop-go\vite.preload.config.ts
  if ($LASTEXITCODE) { throw "preload 构建失败" }
  pnpm exec vite build --config ..\..\apps\desktop-go\vite.renderer.config.ts
  if ($LASTEXITCODE) { throw "renderer 构建失败" }
} finally { Pop-Location }

Write-Host "[2/4] 内置阅读库（reading_libraries.json）"
pnpm dlx tsx apps\desktop-go\jsgen\build-reading-libs.mts
if ($LASTEXITCODE) { throw "阅读库生成失败" }

Write-Host "[3/4] Go 壳（windows/amd64，GUI 子系统）"
$outDir = Join-Path $root $Out
New-Item -ItemType Directory -Force $outDir | Out-Null
Push-Location $shell
try {
  $env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
  # mygo_nodownload：发布版从不联网下载原生库（缺 DLL 时立刻明确报错，而不是静默去 GitHub 拉二进制），与 mygo build 的生产构建一致。
  go build -trimpath -tags mygo_nodownload -ldflags "-s -w -H=windowsgui -X main.packagedBuild=true -X main.appVersion=$version" -o (Join-Path $outDir "GuiZhi.exe") .
  if ($LASTEXITCODE) { throw "go build 失败" }
} finally { Pop-Location; Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue }

Write-Host "[4/4] 汇总产物"
Remove-Item (Join-Path $outDir "dist") -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item $dist (Join-Path $outDir "dist") -Recurse
Copy-Item (Join-Path $shell "reading_libraries.json") $outDir

# MyGo 0.9 起 SQLite 是原生模块（Sqlitecore.dll），必须随发布目录放在 exe 同目录，否则全新机器首次启动必败。
# 取库、校验与下载逻辑集中在 ensure-sqlitecore.ps1（pnpm shot 的隔离构建也用它），原因与细节见该脚本头部。
& (Join-Path $PSScriptRoot "ensure-sqlitecore.ps1") -Out $outDir -GoRoot $shell   # 失败时脚本自己 throw，$ErrorActionPreference=Stop 会中止构建
$size = (Get-ChildItem $outDir -Recurse -File | Measure-Object Length -Sum).Sum / 1MB
Write-Host ("完成：{0}（版本 {1}，共 {2:N1} MB）" -f $outDir, $version, $size)
