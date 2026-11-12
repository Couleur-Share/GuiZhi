# 真实进程验证的公共函数（Windows PowerShell / pwsh 均可）。
# 约束：一切在隔离沙箱（%TEMP% 下临时目录）里离屏运行；不读、不写用户真实的 %APPDATA%\GuiZhi；
# 不按进程名批量结束进程——只等待命令行里带本沙箱路径的 GuiZhi.exe 自行退出，超时才按精确 PID 结束。

$script:ShellRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$script:DefaultExe = Join-Path $ShellRoot 'out\win-x64\GuiZhi.exe'

# New-Sandbox：建一个沙箱目录（ud=userData、appdata=APPDATA 覆盖）。-SourceDb 给出则复制为 ud\data\knowledge.db。
function New-Sandbox {
  param([Parameter(Mandatory)][string]$Name, [string]$SourceDb)
  $root = Join-Path ([IO.Path]::GetTempPath()) "guizhi-verify\$Name"
  if (Test-Path $root) { Remove-Item $root -Recurse -Force }
  New-Item -ItemType Directory -Force "$root\ud\data", "$root\appdata" | Out-Null
  if ($SourceDb) {
    Copy-Item $SourceDb "$root\ud\data\knowledge.db"
  }
  [pscustomobject]@{ Root = $root; UserData = "$root\ud"; AppData = "$root\appdata"; Db = "$root\ud\data\knowledge.db" }
}

# Get-SandboxProcesses：命令行含沙箱根路径的 GuiZhi.exe（含采集子进程）。
function Get-SandboxProcesses {
  param([Parameter(Mandatory)]$Sandbox, [string]$ExeName = 'GuiZhi.exe')
  Get-CimInstance Win32_Process -Filter "Name='$ExeName'" |
    Where-Object { $_.CommandLine -and $_.CommandLine.ToLower().Contains($Sandbox.Root.ToLower()) }
}

# Wait-SandboxExit：等沙箱内进程全部退出；超时则按精确 PID 结束并返回 $false。
function Wait-SandboxExit {
  param([Parameter(Mandatory)]$Sandbox, [int]$TimeoutSec = 120)
  $deadline = (Get-Date).AddSeconds($TimeoutSec)
  while ((Get-Date) -lt $deadline) {
    if (-not (Get-SandboxProcesses $Sandbox)) { return $true }
    Start-Sleep -Milliseconds 500
  }
  foreach ($p in Get-SandboxProcesses $Sandbox) { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue }
  return $false
}

# Invoke-Shot：以 -shot 离屏模式启动 GuiZhi.exe 并等它退出，返回 ExitCode / Stdout / Stderr / Png。
# 只对子进程设置 APPDATA（不改本会话环境变量）。
function Invoke-Shot {
  param(
    [Parameter(Mandatory)]$Sandbox,
    [string]$Exe = $script:DefaultExe,
    [string]$Png = 'o.png',
    [string]$ScriptFile,
    [string]$Wait = '8s',
    [string]$After = '3s',
    [int]$TimeoutSec = 180,
    [hashtable]$ExtraEnv = @{}
  )
  $png = Join-Path $Sandbox.Root $Png
  $argList = @('-shot', $png, '-wait', $Wait, '-userdata', $Sandbox.UserData)
  if ($ScriptFile) { $argList += @('-shotjs', $ScriptFile, '-shotafter', $After) }
  $psi = [Diagnostics.ProcessStartInfo]::new($Exe)
  foreach ($a in $argList) { $psi.ArgumentList.Add($a) }
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.CreateNoWindow = $true
  $psi.Environment['APPDATA'] = $Sandbox.AppData
  foreach ($k in $ExtraEnv.Keys) { $psi.Environment[$k] = $ExtraEnv[$k] }
  $sw = [Diagnostics.Stopwatch]::StartNew()
  $proc = [Diagnostics.Process]::Start($psi)
  $outTask = $proc.StandardOutput.ReadToEndAsync()
  $errTask = $proc.StandardError.ReadToEndAsync()
  $exited = $proc.WaitForExit($TimeoutSec * 1000)
  if (-not $exited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
  $sw.Stop()
  [pscustomobject]@{
    ExitCode = if ($exited) { $proc.ExitCode } else { -1 }
    TimedOut = -not $exited
    Seconds  = [math]::Round($sw.Elapsed.TotalSeconds, 1)
    Stdout   = $outTask.Result
    Stderr   = $errTask.Result
    Pid      = $proc.Id
    Png      = $png
    Results  = @(Get-ChildItem (Join-Path $Sandbox.Root '*.result.json') -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime | ForEach-Object { Get-Content $_.FullName -Raw -Encoding UTF8 | ConvertFrom-Json })
  }
}
