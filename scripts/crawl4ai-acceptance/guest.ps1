param([string]$InputRoot = 'C:\GuiZhiAcceptanceInput', [string]$OutputRoot = 'C:\GuiZhiAcceptanceOutput', [switch]$DisposableVM)
$ErrorActionPreference = 'Stop'
$inputPath = (Resolve-Path -LiteralPath $InputRoot).Path
$manifest = Get-Content -LiteralPath (Join-Path $inputPath 'manifest.json') -Raw | ConvertFrom-Json
if ($env:COMPUTERNAME -eq $manifest.buildHost) { throw 'Refusing to install on the build host.' }
if ($env:USERNAME -ne 'WDAGUtilityAccount' -and !$DisposableVM) { throw 'Use Windows Sandbox or explicitly select a disposable VM.' }
if ([Environment]::OSVersion.Version.Build -lt 22000 -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Windows 11 x64 required.' }
$runPath = Join-Path $OutputRoot ('run-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $runPath | Out-Null
Start-Transcript -Path (Join-Path $runPath 'guest.log') | Out-Null
try {
  foreach ($entry in $manifest.files.PSObject.Properties) {
    $file = [IO.Path]::GetFullPath((Join-Path $inputPath $entry.Name))
    if (!$file.StartsWith($inputPath + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Manifest path escaped input.' }
    if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $entry.Value) { throw "Checksum mismatch: $($entry.Name)" }
  }
  $externalTools = @(Get-Command python,pip,docker -ErrorAction SilentlyContinue | Where-Object { $_.Source -notlike '*\WindowsApps\*' } | Select-Object Name,Source)
  $chrome = @("$env:ProgramFiles\Google\Chrome\Application\chrome.exe", "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe", "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe") | Where-Object { Test-Path -LiteralPath $_ }
  @{ externalTools=$externalTools; chrome=@($chrome); os=[Environment]::OSVersion.Version.ToString(); sandbox=($env:USERNAME -eq 'WDAGUtilityAccount') } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $runPath 'environment.json') -Encoding UTF8
  if ($externalTools.Count -or @($chrome).Count) { throw 'The selected machine is not a clean dependency-free test environment.' }
  $installPath = 'C:\GuiZhiAcceptanceApp'
  if (Test-Path -LiteralPath $installPath) { throw 'Disposable installation directory already exists; use a fresh machine.' }
  $nodePath = Join-Path $inputPath 'tools\driver\node.exe'
  $pythonPath = Join-Path $inputPath 'tools\python\python.exe'
  $shotPath = Join-Path $inputPath 'screenshot.mjs'
  $env:GUIZHI_SHOT_PLAYWRIGHT = Join-Path $inputPath 'tools\driver\package\index.mjs'
  $env:PYTHONDONTWRITEBYTECODE = '1'
  function Invoke-Shot([string]$Phase, [string]$Steps, [string]$Database = '') {
    $arguments = @($shotPath, '--executable', (Join-Path $installPath 'GuiZhi.exe'), '--steps', (Join-Path $inputPath $Steps), '--out', (Join-Path $runPath $Phase), '--keep-profile')
    if ($Database) { $arguments += @('--data-db', $Database) }
    & $nodePath @arguments
    if ($LASTEXITCODE -ne 0) { throw "Shot failed: $Phase" }
  }
  function Get-ProfileDb([string]$Phase) {
    $profile = Get-Content -LiteralPath (Join-Path $runPath "$Phase\profile.json") -Raw | ConvertFrom-Json
    return Join-Path $profile.userDataDir 'data\knowledge.db'
  }
  function Install-Candidate([string]$Name) {
    $process = Start-Process -FilePath (Join-Path $inputPath $Name) -ArgumentList @('/S', '/currentuser', "/D=$installPath") -WindowStyle Hidden -PassThru -Wait
    if ($process.ExitCode -ne 0) { throw "Installer failed: $Name ($($process.ExitCode))" }
    if (!(Test-Path -LiteralPath (Join-Path $installPath 'GuiZhi.exe'))) { throw 'Installed executable missing.' }
  }
  Install-Candidate 'previous.exe'
  Invoke-Shot 'previous-run' 'previous.mjs'
  & $pythonPath (Join-Path $inputPath 'database.py') seed-old --root $runPath
  if ($LASTEXITCODE -ne 0) { throw 'Old database seed failed.' }
  # 保存旧应用，仅用于读取匹配的升级前快照；不读取新 schema。
  Copy-Item -LiteralPath $installPath -Destination (Join-Path $runPath 'previous-app') -Recurse
  Install-Candidate 'candidate.exe'
  $env:GUIZHI_ACCEPTANCE_PREVIOUS = Join-Path $runPath 'previous-run\previous.json'
  Invoke-Shot 'upgrade-run' 'upgrade.mjs' (Join-Path $runPath 'previous-seeded.db')
  & $pythonPath (Join-Path $inputPath 'database.py') verify-upgrade --root $runPath
  if ($LASTEXITCODE -ne 0) { throw 'Migration checks failed.' }
  Invoke-Shot 'capture-final' 'capture.mjs' (Get-ProfileDb 'upgrade-run')
  $env:GUIZHI_ACCEPTANCE_CAPTURE = Join-Path $runPath 'capture-final\capture.json'
  Invoke-Shot 'restored-run' 'restored.mjs' (Get-ProfileDb 'capture-final')
  & $pythonPath (Join-Path $inputPath 'database.py') verify-restored --root $runPath --phase restored-run
  if ($LASTEXITCODE -ne 0) { throw 'Restored table checks failed.' }
  # 再用候选创建全新数据库，验证全新 profile 的组件采集。
  Invoke-Shot 'clean-run' 'capture.mjs'
  $upgrade = Get-Content -LiteralPath (Join-Path $runPath 'upgrade-run\database-check.json') -Raw | ConvertFrom-Json
  & $nodePath $shotPath --executable (Join-Path $runPath 'previous-app\GuiZhi.exe') --steps (Join-Path $inputPath 'rollback.mjs') --data-db $upgrade.backup --out (Join-Path $runPath 'rollback-run') --keep-profile
  if ($LASTEXITCODE -ne 0) { throw 'Previous application backup recovery failed.' }
  @{ passed=$true; installed=$true; source='disposable-guest'; note='Offline controlled HTTP proxy; model and authenticated website tests excluded.' } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $runPath 'result.json') -Encoding UTF8
} catch {
  @{ passed=$false; error=$_.Exception.Message } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $runPath 'result.json') -Encoding UTF8
  throw
} finally { Stop-Transcript | Out-Null }
