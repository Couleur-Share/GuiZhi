#Requires -Version 7
# 把 MyGo 壳的发布目录打成 NSIS 安装包并生成 latest.yml（electron-builder --prepackaged，配置见 mygo-shell\electron-builder.mygo.cjs）。
# 用法（仓库根）：.\spikes\build-installer.ps1 [-Shell spikes\mygo-shell\out\win-x64]
# 先要有 build-shell.ps1 的产物；输出在 spikes\mygo-shell\out\installer。不碰 apps\desktop\out，不发布（--publish never）。
param([string]$Shell = "spikes\mygo-shell\out\win-x64")
$ErrorActionPreference = "Stop"
$root = (Resolve-Path "$PSScriptRoot\..").Path
Set-Location $root
$shellDir = (Resolve-Path $Shell).Path
if (-not (Test-Path (Join-Path $shellDir "GuiZhi.exe"))) { throw "$shellDir 里没有 GuiZhi.exe，请先运行 .\spikes\build-shell.ps1" }
# 没有 SQLite 原生库的包在全新机器（离线或访问不了 GitHub）上首次启动必败，所以拒绝打包。
if (-not (Test-Path (Join-Path $shellDir "mygo-sqlite3.dll"))) { throw "$shellDir 里没有 mygo-sqlite3.dll，请用最新的 .\spikes\build-shell.ps1 重新构建" }
$cfg = Join-Path $root "spikes\mygo-shell\electron-builder.mygo.cjs"
$out = Join-Path $root "spikes\mygo-shell\out\installer"
if (Test-Path $out) { Get-ChildItem $out -File | Remove-Item -Force }
# 构建器会往预打包目录的 resources\ 放 elevate.exe（Electron 版天然有这个目录）。复制到暂存目录再打包，不改动 build-shell 的产物。
$stage = Join-Path $root "spikes\mygo-shell\out\installer-stage"
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
Copy-Item $shellDir $stage -Recurse
New-Item -ItemType Directory -Force (Join-Path $stage "resources") | Out-Null
Push-Location (Join-Path $root "apps\desktop")
try { pnpm exec electron-builder --config $cfg --prepackaged $stage --win nsis --x64 --publish never } finally { Pop-Location }
if ($LASTEXITCODE -ne 0) { throw "electron-builder 失败（退出码 $LASTEXITCODE）" }
Get-ChildItem $out -File | Select-Object Name, @{n = 'MB'; e = { [math]::Round($_.Length / 1MB, 1) } } | Format-Table -AutoSize
"--- latest.yml ---"
Get-Content (Join-Path $out "latest.yml")
