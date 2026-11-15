# 在 Windows 沙盒（来宾）里运行：静默安装 -> 无网络首次启动 -> 取证。由 run-sandbox-fresh-install.ps1 通过 .wsb 的 LogonCommand 拉起。
# 护栏：安装包与真实安装共用固定 guid（会覆盖卸载登记项与安装目录），所以只允许在沙盒用户 WDAGUtilityAccount 下运行。
# 输入：C:\GuiZhiFreshIn（只读映射）里有安装包与 expect.json；输出：C:\GuiZhiFreshOut（可写映射）里写 result.json、guest.log、截图、DONE.flag。
$ErrorActionPreference = 'Continue'
if ($env:USERNAME -ne 'WDAGUtilityAccount') { Write-Error 'refuse to run outside Windows Sandbox'; exit 99 }
$in = 'C:\GuiZhiFreshIn'
$out = 'C:\GuiZhiFreshOut'
New-Item -ItemType Directory -Force $out | Out-Null
Remove-Item "$out\DONE.flag", "$out\result.json" -Force -ErrorAction SilentlyContinue
Start-Transcript -Path "$out\guest.log" -Force | Out-Null
$expect = Get-Content "$in\expect.json" -Raw | ConvertFrom-Json
$r = [ordered]@{}

# 0) 环境事实：无网络、没有 VC++ 运行库（证明原生库不依赖它们）
try { $null = Invoke-WebRequest 'https://github.com' -UseBasicParsing -TimeoutSec 6; $r.networkReachable = $true } catch { $r.networkReachable = $false }
$r.vcruntime140Present = Test-Path 'C:\Windows\System32\vcruntime140.dll'
$r.os = (Get-CimInstance Win32_OperatingSystem).Caption + ' ' + (Get-CimInstance Win32_OperatingSystem).BuildNumber

# 1) 沙盒镜像的 WebView2 登记目录有时与磁盘不一致（见 docs/agents/mygo-migration.md「缺陷 2」）：不一致就按真实目录修正，并如实记录
$k = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$base = 'C:\Program Files (x86)\Microsoft\EdgeWebView\Application'
$r.webview2RegistryRepaired = $false
if ((Test-Path $k) -and (Test-Path $base)) {
  $real = (Get-ChildItem $base -Directory | Where-Object { Test-Path "$($_.FullName)\msedgewebview2.exe" } | Select-Object -First 1).FullName
  $reg = (Get-ItemProperty $k -ErrorAction SilentlyContinue).EBWebView
  if ($real -and $reg -and ($reg -ne $real)) { Set-ItemProperty -Path $k -Name EBWebView -Value $real; $r.webview2RegistryRepaired = $true }
}

# 2) 静默安装
$setup = Get-ChildItem $in -Filter 'GuiZhi-Setup-*.exe' | Select-Object -First 1
$p = Start-Process $setup.FullName -ArgumentList '/S' -Wait -PassThru
$r.installExit = $p.ExitCode
$dir = Join-Path $env:LOCALAPPDATA 'Programs\GuiZhi'
$r.installDir = $dir
$r.exePresent = Test-Path "$dir\GuiZhi.exe"
$dll = "$dir\Sqlitecore.dll"
$r.dllPresent = Test-Path $dll
$r.dllSha256 = if ($r.dllPresent) { (Get-FileHash $dll -Algorithm SHA256).Hash.ToLower() } else { '' }
$r.dllMatchesExpected = ($r.dllSha256 -ne '') -and ($r.dllSha256 -eq $expect.dllSha256)
$r.distPresent = Test-Path "$dir\dist\renderer\index.html"

# 3) 首次启动（不带任何参数，走真实默认数据目录；沙盒是一次性的）
$data = Join-Path $env:APPDATA 'GuiZhi'
$db = Join-Path $data 'data\knowledge.db'
$proc = $null
if ($r.exePresent) { $proc = Start-Process "$dir\GuiZhi.exe" -PassThru }
$deadline = (Get-Date).AddSeconds(60)
while ((Get-Date) -lt $deadline) {
  if ($proc -and $proc.HasExited) { break }
  if ((Test-Path $db) -and $proc) { $proc.Refresh(); if ($proc.MainWindowTitle) { break } }
  Start-Sleep -Milliseconds 500
}
Start-Sleep -Seconds 25   # 让界面渲染完、后台任务起来，再看进程是否稳定
$alive = $false; $title = ''
if ($proc) { $proc.Refresh(); $alive = -not $proc.HasExited; $title = $proc.MainWindowTitle }
$r.processAlive = $alive
$r.windowTitle = $title
$r.dbCreated = Test-Path $db
$hdr = ''
if ($r.dbCreated) {
  try { $fs = [IO.File]::Open($db, 'Open', 'Read', 'ReadWrite'); $b = New-Object byte[] 15; $null = $fs.Read($b, 0, 15); $fs.Dispose(); $hdr = [Text.Encoding]::ASCII.GetString($b) } catch { $hdr = 'read-failed' }
}
$r.dbHeaderOk = ($hdr -eq 'SQLite format 3')
$errLog = Join-Path $data 'logs\error.log'
# 注意：Windows PowerShell 5.1 的 Get-Content -Raw 返回带 PS* 注释属性的字符串，直接进 ConvertTo-Json 会膨胀成几百 KB；用 .NET 读成纯字符串。
$r.errorLog = if (Test-Path $errLog) { [IO.File]::ReadAllText($errLog, [Text.Encoding]::UTF8) } else { '' }
$r.webview2Children = @(Get-Process msedgewebview2 -ErrorAction SilentlyContinue).Count

# 4) 取证：整屏截图
try {
  Add-Type -AssemblyName System.Windows.Forms, System.Drawing
  $v = [System.Windows.Forms.SystemInformation]::VirtualScreen
  $bmp = New-Object System.Drawing.Bitmap $v.Width, $v.Height
  $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($v.Left, $v.Top, 0, 0, $bmp.Size)
  $bmp.Save("$out\screen.png", [System.Drawing.Imaging.ImageFormat]::Png); $g.Dispose(); $bmp.Dispose()
} catch { $r.screenshotError = "$_" }

$r | ConvertTo-Json -Depth 4 | Set-Content "$out\result.json" -Encoding UTF8
Stop-Transcript | Out-Null
New-Item -ItemType File -Path "$out\DONE.flag" -Force | Out-Null
