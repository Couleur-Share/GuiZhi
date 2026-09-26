param([string]$InputRoot = 'C:\GuiZhiAcceptanceInput', [string]$OutputRoot = 'C:\GuiZhiAcceptanceOutput')
$ErrorActionPreference = 'Stop'
$inputPath = (Resolve-Path -LiteralPath $InputRoot).Path
$manifest = Get-Content -LiteralPath (Join-Path $inputPath 'manifest.json') -Raw | ConvertFrom-Json
if ($env:USERNAME -ne 'WDAGUtilityAccount' -or $env:COMPUTERNAME -eq $manifest.buildHost) { throw 'Only run in Windows Sandbox.' }
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Windows x64 required.' }
$installPath = 'C:\GuiZhiMemoryApp'
if (Test-Path -LiteralPath $installPath) { throw 'Use a fresh sandbox.' }
$runPath = Join-Path $OutputRoot ('memory-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $runPath | Out-Null
Start-Transcript -Path (Join-Path $runPath 'guest.log') | Out-Null
try {
  foreach ($entry in $manifest.files.PSObject.Properties) {
    $file = [IO.Path]::GetFullPath((Join-Path $inputPath $entry.Name))
    if (!$file.StartsWith($inputPath + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Manifest path escaped input.' }
    if ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -ne $entry.Value) { throw "Checksum mismatch: $($entry.Name)" }
  }
  $env:GUIZHI_SHOT_PLAYWRIGHT = Join-Path $inputPath 'tools\driver\package\index.mjs'
  $env:PYTHONDONTWRITEBYTECODE = '1'
  $nodePath = Join-Path $inputPath 'tools\driver\node.exe'
  function Assert-NoProcesses {
    $deadline = (Get-Date).AddSeconds(20)
    do {
      $owned = @(Get-Process -Name GuiZhi,python,chrome,node -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($installPath + '\', [StringComparison]::OrdinalIgnoreCase) })
      if (!$owned.Count) { return }
      Start-Sleep -Milliseconds 400
    } while ((Get-Date) -lt $deadline)
    throw 'Owned application processes remain after normal exit.'
  }
  function Install-App([string]$Variant) {
    Assert-NoProcesses
    Write-Host "Installing $Variant"
    $p = Start-Process -FilePath (Join-Path $inputPath "$Variant.exe") -ArgumentList @('/S', '/currentuser', "/D=$installPath") -WindowStyle Hidden -PassThru
    if (!$p.WaitForExit(240000) -or $p.ExitCode -ne 0) { throw 'Installer failed or timed out.' }
    $runtimePath = Join-Path $installPath 'resources\crawl4ai'
    $hasBrowser = Test-Path -LiteralPath (Join-Path $runtimePath 'browser')
    if ($hasBrowser -ne ($Variant -eq 'previous')) { throw 'Installed browser architecture does not match variant.' }
    $runtime = Get-Content -LiteralPath (Join-Path $runtimePath 'manifest.json') -Raw | ConvertFrom-Json
    if ($Variant -eq 'candidate' -and $runtime.renderer -ne 'electron') { throw 'Candidate renderer is wrong.' }
  }
  function Measure-App([string]$Variant, [int]$Round) {
    $phase = "$Variant-$Round"
    Write-Host "Measuring $phase"
    $env:GUIZHI_MEMORY_VARIANT = $Variant
    $shotArguments = @((Join-Path $inputPath 'screenshot.mjs'), '--executable', (Join-Path $installPath 'GuiZhi.exe'), '--steps', (Join-Path $inputPath 'memory-installed.mjs'), '--out', (Join-Path $runPath $phase), '--keep-profile')
    $savedPreference = $ErrorActionPreference
    try {
      $ErrorActionPreference = 'Continue'
      & $nodePath @shotArguments *> (Join-Path $runPath "$phase.log")
      $shotExit = $LASTEXITCODE
    } finally { $ErrorActionPreference = $savedPreference }
    if ($shotExit -ne 0) { throw "Measurement failed: $phase; see log." }
    $result = Get-Content -LiteralPath (Join-Path $runPath "$phase\memory-result.json") -Raw | ConvertFrom-Json
    if (!$result.passed) { throw "Measurement did not pass: $phase" }
    Assert-NoProcesses
    Write-Host "Passed $phase"
  }
  # ABBA 顺序；每轮独立空白知识库，避免数据库大小和执行顺序偏差。
  Install-App 'previous'
  Measure-App 'previous' 1
  Install-App 'candidate'
  Measure-App 'candidate' 1
  Measure-App 'candidate' 2
  Install-App 'previous'
  Measure-App 'previous' 2
  @{ passed=$true; source='windows-sandbox'; order=@('previous-1','candidate-1','candidate-2','previous-2'); sampleIntervalMs=250; normalExit=$true } | ConvertTo-Json | Set-Content (Join-Path $runPath 'result.json') -Encoding UTF8
} catch {
  @{ passed=$false; error=$_.Exception.Message } | ConvertTo-Json | Set-Content (Join-Path $runPath 'result.json') -Encoding UTF8
  throw
} finally { Stop-Transcript | Out-Null }
