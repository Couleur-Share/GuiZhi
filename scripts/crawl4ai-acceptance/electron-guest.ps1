param([string]$InputRoot = 'C:\GuiZhiAcceptanceInput', [string]$OutputRoot = 'C:\GuiZhiAcceptanceOutput')
$ErrorActionPreference = 'Stop'
$inputPath = (Resolve-Path -LiteralPath $InputRoot).Path
$manifest = Get-Content -LiteralPath (Join-Path $inputPath 'manifest.json') -Raw | ConvertFrom-Json
if ($env:USERNAME -ne 'WDAGUtilityAccount' -or $env:COMPUTERNAME -eq $manifest.buildHost) { throw 'Only run inside Windows Sandbox; never install on the host.' }
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Windows x64 required.' }
$installPath = 'C:\GuiZhiAcceptanceApp'
if (Test-Path -LiteralPath $installPath) { throw 'Use a fresh sandbox.' }
$runPath = Join-Path $OutputRoot ('electron-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $runPath | Out-Null
Start-Transcript -Path (Join-Path $runPath 'guest.log') | Out-Null
try {
  foreach ($entry in $manifest.files.PSObject.Properties) {
    $file = [IO.Path]::GetFullPath((Join-Path $inputPath $entry.Name))
    if (!$file.StartsWith($inputPath + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Manifest path escaped input.' }
    if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $entry.Value) { throw "Checksum mismatch: $($entry.Name)" }
  }
  $external = @(Get-Command python,pip,docker -ErrorAction SilentlyContinue | Where-Object { $_.Source -notlike '*\WindowsApps\*' })
  $chrome = @("$env:ProgramFiles\Google\Chrome\Application\chrome.exe", "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe", "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe") | Where-Object { Test-Path -LiteralPath $_ }
  if ($external.Count -or @($chrome).Count) { throw 'Guest is not dependency-free.' }
  @{ os=[Environment]::OSVersion.Version.ToString(); externalTools=$external.Count; externalChrome=@($chrome).Count; sandbox=$true } | ConvertTo-Json | Set-Content (Join-Path $runPath 'environment.json') -Encoding UTF8
  $nodePath = Join-Path $inputPath 'tools\driver\node.exe'
  $env:GUIZHI_SHOT_PLAYWRIGHT = Join-Path $inputPath 'tools\driver\package\index.mjs'
  $env:PYTHONDONTWRITEBYTECODE = '1'
  function Install-App([string]$Name) {
    Write-Host "Installing $Name"
    $p = Start-Process -FilePath (Join-Path $inputPath $Name) -ArgumentList @('/S', '/currentuser', "/D=$installPath") -WindowStyle Hidden -PassThru
    if (!$p.WaitForExit(240000)) { throw 'Installer timed out.' }
    if ($p.ExitCode -ne 0) { throw "Installer failed: $($p.ExitCode)" }
    if (!(Test-Path -LiteralPath (Join-Path $installPath 'GuiZhi.exe'))) { throw 'Installed executable missing.' }
  }
  function Assert-NoProcesses {
    $deadline = (Get-Date).AddSeconds(20)
    do {
      $owned = @(Get-Process -Name GuiZhi,python,chrome -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($installPath + '\', [StringComparison]::OrdinalIgnoreCase) })
      if (!$owned.Count) { return }
      Start-Sleep -Milliseconds 400
    } while ((Get-Date) -lt $deadline)
    throw 'Application or capture processes remain after normal exit.'
  }
  function Invoke-Phase([string]$Phase, [string]$Database = '') {
    Write-Host "Testing $Phase"
    $env:GUIZHI_INSTALLED_PHASE = $Phase
    $env:GUIZHI_INSTALLED_EXPECTED_VERSION = if ($Phase -eq 'previous') { $manifest.previousVersion } else { $manifest.candidateVersion }
    $shotArguments = @((Join-Path $inputPath 'screenshot.mjs'), '--executable', (Join-Path $installPath 'GuiZhi.exe'), '--steps', (Join-Path $inputPath 'electron-installed.mjs'), '--out', (Join-Path $runPath $Phase), '--keep-profile')
    if ($Database) { $shotArguments += @('--data-db', $Database) }
    $savedPreference = $ErrorActionPreference
    try {
      $ErrorActionPreference = 'Continue'
      & $nodePath @shotArguments *> (Join-Path $runPath "$Phase.log")
      $shotExit = $LASTEXITCODE
    } finally { $ErrorActionPreference = $savedPreference }
    if ($shotExit -ne 0) { throw "Phase failed: $Phase; see $Phase.log" }
    if (!(Test-Path -LiteralPath (Join-Path $runPath "$Phase\installed.json"))) { throw "Phase produced no acceptance result: $Phase" }
    Assert-NoProcesses
  }
  function Assert-ElectronRuntime {
    $runtime = Join-Path $installPath 'resources\crawl4ai'
    $value = Get-Content -LiteralPath (Join-Path $runtime 'manifest.json') -Raw | ConvertFrom-Json
    if ($value.renderer -ne 'electron' -or $value.browser -or (Test-Path -LiteralPath (Join-Path $runtime 'browser'))) { throw 'Standalone Chromium remains after installation.' }
    if (!$value.workerHashes.'extract-only.py') { throw 'Extractor missing from installed manifest.' }
  }
  Install-App 'previous.exe'
  if (!(Test-Path -LiteralPath (Join-Path $installPath 'resources\crawl4ai\browser'))) { throw 'Previous installer is not the standalone Chromium baseline.' }
  Invoke-Phase 'previous'
  $profile = Get-Content -LiteralPath (Join-Path $runPath 'previous\profile.json') -Raw | ConvertFrom-Json
  $dbPath = [IO.Path]::GetFullPath((Join-Path $profile.userDataDir 'data\knowledge.db'))
  if (!$dbPath.StartsWith($runPath + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Test database escaped output.' }
  $dbHash = (Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash
  Install-App 'candidate.exe'
  Assert-ElectronRuntime
  if ((Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash -ne $dbHash) { throw 'Installer changed existing test database.' }
  $env:GUIZHI_INSTALLED_PREVIOUS = Join-Path $runPath 'previous\installed.json'
  Invoke-Phase 'upgrade' $dbPath
  # 正常卸载后重新安装，覆盖全新安装和升级安装两条路径。
  $uninstallers = @(Get-ChildItem -LiteralPath $installPath -File | Where-Object { $_.Name -match '^Uninstall.*\.exe$' })
  if ($uninstallers.Count -ne 1) { throw 'Expected one owned uninstaller.' }
  $p = Start-Process -FilePath $uninstallers[0].FullName -ArgumentList '/S' -WindowStyle Hidden -PassThru
  if (!$p.WaitForExit(120000) -or $p.ExitCode -ne 0) { throw 'Uninstall failed.' }
  $deadline = (Get-Date).AddSeconds(60)
  while ((Test-Path -LiteralPath (Join-Path $installPath 'resources')) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
  if (Test-Path -LiteralPath (Join-Path $installPath 'resources')) { throw 'Program resources remain after uninstall.' }
  Assert-NoProcesses
  if ((Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash -ne $dbHash) { throw 'Uninstaller changed saved knowledge.' }
  Install-App 'candidate.exe'
  Assert-ElectronRuntime
  Invoke-Phase 'clean'
  @{ passed=$true; source='windows-sandbox'; previousVersion=$manifest.previousVersion; candidateVersion=$manifest.candidateVersion; upgradeKind=$(if ($manifest.previousVersion -eq $manifest.candidateVersion) { 'same-version replacement' } else { 'version upgrade' }); oldDataPreserved=$true; oldWebVersionsPreserved=$true; standaloneChromiumRemoved=$true; cleanInstall=$true; staticCapture=$true; dynamicCapture=$true; idleCleanup=$true; normalExit=$true; previousDatabaseSha256=$dbHash } | ConvertTo-Json | Set-Content (Join-Path $runPath 'result.json') -Encoding UTF8
} catch {
  @{ passed=$false; error=$_.Exception.Message } | ConvertTo-Json | Set-Content (Join-Path $runPath 'result.json') -Encoding UTF8
  throw
} finally { Stop-Transcript | Out-Null }
