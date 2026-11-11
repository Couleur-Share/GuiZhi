# AI 代理差分测试：同一批场景、同一个模拟服务，分别跑 TS 原版（真实 ai.ipc.ts + 真实 preload，electron 替身）
# 和 Go 版（MyGo 壳层 -aitest，真实 preload 跑在 WebView2 页面里），再比较。
# 用法（仓库根目录）：powershell -File spikes\mygo-ai\run-diff.ps1 -ShellExe <壳层.exe> -Dist <dist> -DbFile <fixture 数据库>
param([Parameter(Mandatory)][string]$ShellExe, [Parameter(Mandatory)][string]$Dist, [Parameter(Mandatory)][string]$DbFile)
$ErrorActionPreference = 'Continue'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$spikes = Split-Path -Parent $here
$work = Join-Path $env:TEMP ("guizhi-aidiff-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force "$work\wv2" | Out-Null
Copy-Item $DbFile "$work\knowledge.db"
$mock = Start-Process -FilePath (Join-Path $spikes 'mygo-ai-mock.exe') -PassThru -RedirectStandardOutput "$work\mock.txt" -WindowStyle Hidden
try {
  Start-Sleep -Seconds 1
  $base = ((Get-Content "$work\mock.txt" | Select-Object -First 1) -replace '^MOCK ', '')
  $env:WEBVIEW2_USER_DATA_FOLDER = "$work\wv2"
  # 两侧各自一个模拟服务实例由同一进程提供，互不影响（场景无服务端状态）
  & $ShellExe -dist $Dist -db "$work\knowledge.db" -aitest $base -aiscen "$here\scenarios.json" -airunner "$here\runner.mjs" -aiout "$work\go-out.json" > "$work\go-log.txt" 2>&1; Get-Content "$work\go-log.txt" | Where-Object { $_ -match 'FAIL|OK\]' }
  Remove-Item Env:WEBVIEW2_USER_DATA_FOLDER
  Push-Location (Split-Path -Parent $spikes)
  pnpm dlx tsx "$here\ts-run.mts" $base "$work\ts-out.json" 2>&1 | Where-Object { $_ -match '基准完成|Error' }
  Pop-Location
  node "$here\compare.mjs" "$work\ts-out.json" "$work\go-out.json"
  Write-Host "结果目录：$work"
} finally { Stop-Process -Id $mock.Id -ErrorAction SilentlyContinue }
