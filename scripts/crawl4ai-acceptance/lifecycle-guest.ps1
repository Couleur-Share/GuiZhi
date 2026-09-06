param([string]$InputRoot = 'C:\GuiZhiAcceptanceInput', [string]$ScriptRoot = 'C:\GuiZhiLifecycleInput', [string]$OutputRoot = 'C:\GuiZhiLifecycleOutput')
$ErrorActionPreference = 'Stop'
if ($env:USERNAME -ne 'WDAGUtilityAccount') { throw 'This installer/uninstaller test requires Windows Sandbox.' }
if ($env:COMPUTERNAME -eq 'COULEUR') { throw 'Refusing to install on the development host.' }
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Windows x64 required.' }
$installPath = 'C:\GuiZhiAcceptanceApp'
if (Test-Path -LiteralPath $installPath) { throw 'Use a fresh sandbox; preserve the existing installation.' }
$runPath = Join-Path $OutputRoot ('lifecycle-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $runPath | Out-Null
Start-Transcript -Path (Join-Path $runPath 'guest.log') | Out-Null
try {
  $manifest = Get-Content -LiteralPath (Join-Path $ScriptRoot 'manifest.json') -Raw | ConvertFrom-Json
  foreach ($entry in $manifest.files.PSObject.Properties) {
    $file = [IO.Path]::GetFullPath((Join-Path $ScriptRoot $entry.Name))
    if (!$file.StartsWith($ScriptRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Script path escaped input.' }
    if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $entry.Value) { throw "Checksum mismatch: $($entry.Name)" }
  }
  $installer = Join-Path $InputRoot 'candidate.exe'
  if ((Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash -ne $manifest.candidateSha256) { throw 'Candidate checksum mismatch.' }
  Write-Host '[1/5] 沙盒内安装候选 / Install candidate'
  $installed = Start-Process -FilePath $installer -ArgumentList @('/S', '/currentuser', "/D=$installPath") -WindowStyle Hidden -PassThru
  if (!$installed.WaitForExit(120000)) { throw 'Installer timeout' }
  if ($installed.ExitCode -ne 0) { throw "Installer failed: $($installed.ExitCode)" }
  $nodePath = Join-Path $InputRoot 'tools\driver\node.exe'
  $env:GUIZHI_SHOT_PLAYWRIGHT = Join-Path $InputRoot 'tools\driver\package\index.mjs'
  $shotPath = Join-Path $ScriptRoot 'screenshot.mjs'
  function Invoke-Shot([string]$Phase, [string]$Steps) {
    & $nodePath $shotPath --executable (Join-Path $installPath 'GuiZhi.exe') --steps (Join-Path $ScriptRoot $Steps) --out (Join-Path $runPath $Phase) --keep-profile *> (Join-Path $runPath "$Phase.log")
    if ($LASTEXITCODE -ne 0) { throw "Test failed: $Phase; see $Phase.log" }
  }
  Write-Host '[2/5] 验证真实恢复重启 / Observe unmodified app.relaunch'
  Invoke-Shot 'relaunch' 'relaunch.mjs'
  Write-Host '[3/5] 采集一页后正常退出 / Capture one page and quit'
  Invoke-Shot 'collect' 'collect-one.mjs'
  function Get-OwnedProcesses {
    @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($installPath + '\', [StringComparison]::OrdinalIgnoreCase) } | Select-Object ProcessId,Name,ExecutablePath)
  }
  $deadline = (Get-Date).AddSeconds(15)
  do { $remaining = @(Get-OwnedProcesses); if (!$remaining.Count) { break }; Start-Sleep -Milliseconds 300 } while ((Get-Date) -lt $deadline)
  if ($remaining.Count) { throw 'Owned application or crawler processes remain after normal exit.' }
  $profile = Get-Content -LiteralPath (Join-Path $runPath 'relaunch\profile.json') -Raw | ConvertFrom-Json
  $dbPath = [IO.Path]::GetFullPath((Join-Path $profile.userDataDir 'data\knowledge.db'))
  if (!$dbPath.StartsWith($runPath + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Test data escaped output.' }
  $dbHash = (Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash
  $sentinelRoot = Join-Path $env:APPDATA 'GuiZhi'
  New-Item -ItemType Directory -Path $sentinelRoot -Force | Out-Null
  $sentinel = Join-Path $sentinelRoot 'acceptance-user-data.txt'
  if (Test-Path -LiteralPath $sentinel) { throw 'Preserve existing sentinel.' }
  'Synthetic user data: retain on ordinary uninstall.' | Set-Content -LiteralPath $sentinel -Encoding UTF8
  $sentinelHash = (Get-FileHash -LiteralPath $sentinel -Algorithm SHA256).Hash
  $uninstallers = @(Get-ChildItem -LiteralPath $installPath -File | Where-Object { $_.Name -match '^Uninstall.*\.exe$' })
  if ($uninstallers.Count -ne 1) { throw 'Expected exactly one owned uninstaller.' }
  Write-Host '[4/5] 卸载沙盒内安装 / Uninstall'
  $uninstalled = Start-Process -FilePath $uninstallers[0].FullName -ArgumentList '/S' -WindowStyle Hidden -PassThru
  if (!$uninstalled.WaitForExit(90000)) { throw 'Uninstaller timeout' }
  if ($uninstalled.ExitCode -ne 0) { throw "Uninstaller failed: $($uninstalled.ExitCode)" }
  $deadline = (Get-Date).AddSeconds(90)
  while ((Test-Path -LiteralPath (Join-Path $installPath 'resources')) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
  if ((Test-Path -LiteralPath (Join-Path $installPath 'GuiZhi.exe')) -or (Test-Path -LiteralPath (Join-Path $installPath 'resources'))) { throw 'Application resources remain after uninstall.' }
  if (@(Get-OwnedProcesses).Count) { throw 'Owned processes remain after uninstall.' }
  $registrations = @(Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -like 'GuiZhi*' })
  if ($registrations.Count -or (Test-Path 'HKCU:\Software\GuiZhi\InstallerState')) { throw 'Installer registration remains.' }
  if (Test-Path -LiteralPath (Join-Path $env:USERPROFILE 'Desktop\GuiZhi.lnk')) { throw 'Desktop shortcut remains.' }
  if ((Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash -ne $dbHash) { throw 'Test knowledge changed during uninstall.' }
  if ((Get-FileHash -LiteralPath $sentinel -Algorithm SHA256).Hash -ne $sentinelHash) { throw 'Default user data was deleted or changed.' }
  Write-Host '[5/5] 生命周期验收通过 / Passed'
  @{ passed=$true; source='sandbox-lifecycle'; actualRelaunch=$true; crawlerExit=$true; uninstall=$true; programResourcesRemoved=$true; userDataPreserved=$true; candidateSha256=$manifest.candidateSha256; databaseSha256=$dbHash } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $runPath 'result.json') -Encoding UTF8
} catch {
  @{ passed=$false; error=$_.Exception.Message } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $runPath 'result.json') -Encoding UTF8
  throw
} finally { Stop-Transcript | Out-Null }
