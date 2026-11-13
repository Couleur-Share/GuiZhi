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
  go build -trimpath -ldflags "-s -w -H=windowsgui -X main.packagedBuild=true -X main.appVersion=$version" -o (Join-Path $outDir "GuiZhi.exe") .
  if ($LASTEXITCODE) { throw "go build 失败" }
} finally { Pop-Location; Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue }

Write-Host "[4/4] 汇总产物"
Remove-Item (Join-Path $outDir "dist") -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item $dist (Join-Path $outDir "dist") -Recurse
Copy-Item (Join-Path $shell "reading_libraries.json") $outDir

# MyGo 的 SQLite 插件按「资源目录 → exe 同目录 → 用户缓存 → 联网下载」找 mygo-sqlite3.dll。
# 普通 go build 不会把它带进包里：开发机因为有缓存所以能跑，全新机器（离线或访问不了 GitHub）首次启动会直接失败。
# 所以发布目录必须自带这份 DLL，并按插件清单里的 SHA-256 校验（插件加载时也会再校验一次）。
Write-Host "      内置 SQLite 原生库（mygo-sqlite3.dll）"
Push-Location $shell
try { $mygoDir = (go list -m -f '{{.Dir}}' github.com/egoist/mygo).Trim() } finally { Pop-Location }
$manifest = Get-Content (Join-Path $mygoDir "plugins\sqlite\mygo-plugin.json") -Raw | ConvertFrom-Json
$native = $manifest.libraries[0].files.'windows-amd64'
$dllDest = Join-Path $outDir $native.name
function Test-Sha256([string]$path, [string]$want) {
  (Test-Path -LiteralPath $path) -and ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ieq $want)
}
$dllCache = Join-Path $env:LOCALAPPDATA ("mygo\natives\{0}\{1}" -f $native.sha256, $native.name)
if (-not (Test-Sha256 $dllCache $native.sha256)) {
  Write-Host "  缓存里没有校验通过的原生库，从清单地址下载：$($native.url)"
  New-Item -ItemType Directory -Force (Split-Path $dllCache) | Out-Null
  $tmp = "$dllCache.part"
  Invoke-WebRequest -Uri $native.url -OutFile $tmp
  if (-not (Test-Sha256 $tmp $native.sha256)) { Remove-Item $tmp -Force; throw "mygo-sqlite3.dll 的 SHA-256 与插件清单不一致，已丢弃下载内容" }
  Move-Item $tmp $dllCache -Force
}
Copy-Item $dllCache $dllDest -Force
if (-not (Test-Sha256 $dllDest $native.sha256)) { throw "复制后的 $($native.name) 校验失败" }
$size = (Get-ChildItem $outDir -Recurse -File | Measure-Object Length -Sum).Sum / 1MB
Write-Host ("完成：{0}（版本 {1}，共 {2:N1} MB）" -f $outDir, $version, $size)