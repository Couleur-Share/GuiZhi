# 构建 MyGo 壳的 Windows x64 发布目录：<out>\GuiZhi.exe + dist\{renderer,preload} + reading_libraries.json
# 用法（仓库根）：.\spikes\build-shell.ps1 [-Out spikes\mygo-shell\out\win-x64]
# 不碰 apps\desktop\out（Electron 构建产物），可在开发服务运行时执行。
param([string]$Out = "spikes\mygo-shell\out\win-x64")
$ErrorActionPreference = "Stop"
$root = (Resolve-Path "$PSScriptRoot\..").Path
Set-Location $root
$version = (Get-Content apps\desktop\package.json -Raw | ConvertFrom-Json).version
$shell = Join-Path $root "spikes\mygo-shell"
$dist = Join-Path $shell "dist"

Write-Host "[1/4] 渲染进程与 preload（vite）"
Push-Location apps\desktop
try {
  pnpm exec vite build --config ..\..\spikes\mygo-shell\vite.preload.config.ts
  if ($LASTEXITCODE) { throw "preload 构建失败" }
  pnpm exec vite build --config ..\..\spikes\mygo-shell\vite.renderer.config.ts
  if ($LASTEXITCODE) { throw "renderer 构建失败" }
} finally { Pop-Location }

Write-Host "[2/4] 内置阅读库（reading_libraries.json）"
pnpm dlx tsx spikes\mygo-js\build-reading-libs.mts
if ($LASTEXITCODE) { throw "阅读库生成失败" }

Write-Host "[3/4] Go 壳（windows/amd64，GUI 子系统）"
$outDir = Join-Path $root $Out
New-Item -ItemType Directory -Force $outDir | Out-Null
Push-Location $shell
try {
  $env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
  go build -trimpath -ldflags "-s -w -H=windowsgui -X main.packagedBuild=true -X main.appVersion=$version" -o (Join-Path $outDir "GuiZhi.exe") .
  if ($LASTEXITCODE) { throw "go build 失败" }
} finally { Pop-Location; Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue }

Write-Host "[4/4] 汇总产物"
Remove-Item (Join-Path $outDir "dist") -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item $dist (Join-Path $outDir "dist") -Recurse
Copy-Item (Join-Path $shell "reading_libraries.json") $outDir
$size = (Get-ChildItem $outDir -Recurse -File | Measure-Object Length -Sum).Sum / 1MB
Write-Host ("完成：{0}（版本 {1}，共 {2:N1} MB）" -f $outDir, $version, $size)