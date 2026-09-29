# 归知鸿蒙端真机命令行回路（hdc）。默认只操作开发包（.dev），不触碰正式包；详见 docs/device-loop.md。
# 隔离：hdc 服务使用独立端口，只对本进程生效，不会结束或抢占 HoKit / DevEco 使用的默认服务。
# 注意：脚本内不要使用中文弯引号，Windows PowerShell 5.1 会把它们当作字符串定界符。
param(
  [Parameter(Position = 0, Mandatory = $true)]
  [ValidateSet('targets', 'install', 'launch', 'stop', 'shot', 'dump', 'tap', 'key', 'logs', 'server-stop')]
  [string]$Command,
  [string]$Bundle = 'com.couleurapp.guizhi.capture.dev',
  # launch 要启动的 Ability；分享诊断页用 ShareDiagAbility（仅 dev 包内置）。
  [string]$Ability = 'EntryAbility',
  [string]$Hap,
  [string]$Name = 'shot',
  [string]$Text,
  [int]$X = -1,
  [int]$Y = -1,
  [int]$Index = 0,
  [string]$Key,
  [int]$Lines = 200,
  [string]$Filter = 'guizhi',
  [switch]$Clear,
  [string]$Device,
  [string]$Hdc,
  [string]$ToolsRoot = $env:HARMONY_COMMAND_LINE_TOOLS,
  [int]$ServerPort = 18710,
  [string]$OutDir,
  # 允许操作正式包（默认拒绝，避免误覆盖或清除正式版本）。
  [switch]$Force
)
$ErrorActionPreference = 'Stop'
# 管道或重定向时 Windows PowerShell 5.1 默认用系统代码页输出，中文会乱码。
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch { }
$productionBundle = 'com.couleurapp.guizhi.capture'
$devBundle = 'com.couleurapp.guizhi.capture.dev'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
if (-not $OutDir) { $OutDir = Join-Path $repoRoot 'artifacts/harmony' }

function Fail([string]$Message, [int]$Code = 1) {
  [Console]::Error.WriteLine($Message)
  exit $Code
}

# 参数会拼进设备端 shell 命令，先做白名单校验，拒绝空格、分号、引号等。
if ($Bundle -notmatch '^[A-Za-z][A-Za-z0-9_.]{2,127}$') { Fail "包名格式不合法：$Bundle" 2 }
if ($Bundle -eq $productionBundle -and -not $Force) {
  Fail "拒绝操作正式包 $productionBundle。默认只操作开发包 $devBundle；确需操作正式包请显式加 -Force。" 2
}
if ($Ability -notmatch '^[A-Za-z][A-Za-z0-9_]{1,63}$') { Fail "Ability 名称格式不合法：$Ability" 2 }
if ($Device -and $Device -notmatch '^[A-Za-z0-9._:\-\[\]]{1,80}$') { Fail "设备标识格式不合法：$Device" 2 }
if ($Name -notmatch '^[A-Za-z0-9_\-]{1,40}$') { Fail '名称只能包含字母、数字、下划线和连字符（最长 40）。' 2 }
if ($Key -and $Key -notmatch '^[A-Za-z0-9]+( [A-Za-z0-9]+)?$') { Fail '按键只能是键名或最多两个键值，例如 Back、Home、2072 2038。' 2 }

# 定位 hdc：-Hdc > 环境变量 GUIZHI_HDC > 命令行工具包 > PATH。
$hdcPath = $Hdc
if (-not $hdcPath) { $hdcPath = $env:GUIZHI_HDC }
if (-not $hdcPath -and $ToolsRoot) {
  $candidate = Join-Path $ToolsRoot 'sdk/default/openharmony/toolchains/hdc.exe'
  if (Test-Path -LiteralPath $candidate) { $hdcPath = $candidate }
}
if (-not $hdcPath) {
  $found = Get-Command hdc -ErrorAction SilentlyContinue
  if ($found) { $hdcPath = $found.Source }
}
if (-not $hdcPath -or -not (Test-Path -LiteralPath $hdcPath)) {
  Fail '未找到 hdc。请用 -Hdc 指定，或设置 GUIZHI_HDC / HARMONY_COMMAND_LINE_TOOLS 环境变量。' 2
}

$previousPort = $env:OHOS_HDC_SERVER_PORT
$env:OHOS_HDC_SERVER_PORT = "$ServerPort"
$script:target = ''

function Invoke-Hdc([string[]]$Arguments, [switch]$AllowFailure) {
  $all = @()
  if ($script:target) { $all += @('-t', $script:target) }
  $all += $Arguments
  # Windows PowerShell 5.1 在 Stop 模式下会把原生命令的 stderr 当作终止错误。
  $before = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { $output = & $hdcPath @all 2>&1 | ForEach-Object { "$_" } }
  finally { $ErrorActionPreference = $before }
  $code = $LASTEXITCODE
  if ($code -ne 0 -and -not $AllowFailure) {
    Fail ("hdc 命令失败（退出码 $code）：hdc " + ($Arguments -join ' ') + "`n" + (@($output) -join "`n")) 1
  }
  return @{ Code = $code; Lines = @($output | Where-Object { $null -ne $_ }) }
}

function Resolve-Target {
  if ($Device) { return $Device }
  $result = Invoke-Hdc @('list', 'targets') -AllowFailure
  $targets = @($result.Lines | ForEach-Object { $_.Trim() } | Where-Object { $_ -and $_ -notmatch '^\[Empty\]' })
  if ($targets.Count -eq 0) {
    Fail '未发现已授权设备。请确认 USB 已连接、手机已开启 USB 调试并点了允许调试；HoKit 或 DevEco 若占着设备，请先关闭。' 3
  }
  if ($targets.Count -gt 1) { Fail ("发现多台设备，请用 -Device 指定其一：`n" + ($targets -join "`n")) 3 }
  return $targets[0]
}

function New-OutputPath([string]$Folder, [string]$Extension) {
  $dir = Join-Path $OutDir $Folder
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  return Join-Path $dir ((Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + $Name + $Extension)
}

# 从设备取回文件并删除设备端临时文件；返回本地路径。
function Receive-DeviceFile([string]$Remote, [string]$LocalPath) {
  Invoke-Hdc @('file', 'recv', $Remote, $LocalPath) | Out-Null
  Invoke-Hdc @('shell', 'rm', $Remote) -AllowFailure | Out-Null
  if (-not (Test-Path -LiteralPath $LocalPath) -or (Get-Item -LiteralPath $LocalPath).Length -le 0) {
    Fail "从设备取回文件失败或文件为空：$Remote" 1
  }
}

function Dump-Layout([string]$LocalPath) {
  $result = Invoke-Hdc @('shell', 'uitest', 'dumpLayout')
  $joined = $result.Lines -join "`n"
  $match = [regex]::Match($joined, 'DumpLayout saved to:\s*(\S+)')
  if (-not $match.Success) { Fail "读取界面布局失败（应用需在前台）：`n$joined" 1 }
  Receive-DeviceFile $match.Groups[1].Value $LocalPath
}

function Find-Hap {
  if ($Hap) {
    if (-not (Test-Path -LiteralPath $Hap)) { Fail "找不到 HAP：$Hap" 2 }
    return (Resolve-Path -LiteralPath $Hap).Path
  }
  $product = if ($Bundle -eq $productionBundle) { 'default' } else { 'dev' }
  $folder = Join-Path $repoRoot "artifacts/harmony/hap/$product"
  $signed = @()
  if (Test-Path -LiteralPath $folder) {
    $signed = @(Get-ChildItem -LiteralPath $folder -Filter '*.hap' -File | Where-Object { $_.Name -notmatch 'unsigned' } | Sort-Object LastWriteTime -Descending)
  }
  if ($signed.Count -eq 0) {
    Fail "没有已签名的 HAP（$folder）。请先按 docs/device-loop.md 配置调试签名，再运行 build.ps1 -Product $product -Isolated -Build。" 2
  }
  return $signed[0].FullName
}

function Protect-Log([string]$Line) {
  # 日志只用于排错：抹掉链接、令牌和凭证类字段，避免把正文或密钥带出设备。
  $line = [regex]::Replace($Line, 'https?://\S+', '<url>')
  $line = [regex]::Replace($line, '(?i)bearer\s+\S+', 'Bearer <redacted>')
  return [regex]::Replace($line, '(?i)(credential|nonce|token|password)\w*\s*[:=]\s*\S+', '$1=<redacted>')
}

try {
  switch ($Command) {
    'targets' {
      $result = Invoke-Hdc @('list', 'targets') -AllowFailure
      $result.Lines | ForEach-Object { Write-Output $_ }
      if ($result.Code -ne 0) { Fail "hdc 退出码 $($result.Code)" 1 }
    }
    'server-stop' {
      # 只结束本脚本自己端口上的 hdc 服务，不影响默认端口的服务。
      $result = Invoke-Hdc @('kill') -AllowFailure
      $result.Lines | ForEach-Object { Write-Output $_ }
    }
    'install' {
      $script:target = Resolve-Target
      $file = Find-Hap
      if ((Split-Path $file -Leaf) -match 'unsigned') { Fail "拒绝安装未签名的 HAP：$file" 2 }
      $result = Invoke-Hdc @('install', '-r', $file) -AllowFailure
      $joined = $result.Lines -join "`n"
      Write-Output $joined
      if ($joined -notmatch 'install bundle successfully') { Fail '安装未成功，请检查上面的 hdc 输出（常见原因：签名与包名不匹配）。' 1 }
      Write-Output "已安装：$Bundle"
    }
    'launch' {
      $script:target = Resolve-Target
      $result = Invoke-Hdc @('shell', 'aa', 'start', '-a', $Ability, '-b', $Bundle) -AllowFailure
      $joined = $result.Lines -join "`n"
      Write-Output $joined
      if ($joined -notmatch 'start ability successfully') { Fail "启动失败，请确认应用已安装且包含 ${Ability}。" 1 }
    }
    'stop' {
      $script:target = Resolve-Target
      $result = Invoke-Hdc @('shell', 'aa', 'force-stop', $Bundle) -AllowFailure
      $result.Lines | ForEach-Object { Write-Output $_ }
    }
    'shot' {
      $script:target = Resolve-Target
      $local = New-OutputPath 'shots' '.jpeg'
      $remote = '/data/local/tmp/guizhi-shot.jpeg'
      Invoke-Hdc @('shell', 'snapshot_display', '-f', $remote) | Out-Null
      Receive-DeviceFile $remote $local
      Write-Output "产物：$local"
    }
    'dump' {
      $script:target = Resolve-Target
      $local = New-OutputPath 'dumps' '.json'
      Dump-Layout $local
      Write-Output "产物：$local"
      $node = Get-Command node -ErrorAction SilentlyContinue
      if ($node) {
        Write-Output '界面文字（按控件树顺序）：'
        & $node.Source (Join-Path $PSScriptRoot 'layout-find.mjs') --texts $local | Select-Object -First 80 | ForEach-Object { Write-Output "  $_" }
      }
    }
    'tap' {
      $script:target = Resolve-Target
      $px = $X; $py = $Y
      if ($Text) {
        $node = Get-Command node -ErrorAction SilentlyContinue
        if (-not $node) { Fail '按文字点击需要 Node.js（用于解析布局）。' 2 }
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('guizhi-layout-' + [guid]::NewGuid().ToString('N') + '.json')
        try {
          Dump-Layout $tmp
          $found = & $node.Source (Join-Path $PSScriptRoot 'layout-find.mjs') $tmp $Text --index $Index
          if ($LASTEXITCODE -ne 0) { Fail "界面上没有找到「$Text」（索引 $Index）。" 3 }
        } finally { if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Force } }
        $parts = @("$found".Trim() -split '\s+')
        if ($parts.Count -ne 2) { Fail "无法解析点击坐标：$found" 1 }
        $px = [int]$parts[0]; $py = [int]$parts[1]
      }
      if ($px -lt 0 -or $py -lt 0) { Fail '请提供 -Text，或同时提供 -X 与 -Y。' 2 }
      Invoke-Hdc @('shell', 'uitest', 'uiInput', 'click', "$px", "$py") | Out-Null
      Write-Output "已点击 ($px, $py)"
    }
    'key' {
      if (-not $Key) { Fail '请用 -Key 指定按键，例如 -Key Back。' 2 }
      $script:target = Resolve-Target
      Invoke-Hdc (@('shell', 'uitest', 'uiInput', 'keyEvent') + @($Key -split ' ')) | Out-Null
      Write-Output "已发送按键 $Key"
    }
    'logs' {
      $script:target = Resolve-Target
      if ($Clear) {
        Invoke-Hdc @('shell', 'hilog', '-r') -AllowFailure | Out-Null
        Write-Output '已清空设备日志缓冲。'
      } else {
        $result = Invoke-Hdc @('shell', 'hilog', '-x')
        $matched = @($result.Lines | Where-Object { $_ -match $Filter } | Select-Object -Last $Lines)
        Write-Output "匹配 /$Filter/ 的最近 $($matched.Count) 行（已脱敏）："
        $matched | ForEach-Object { Write-Output (Protect-Log $_) }
      }
    }
  }
} finally {
  if ($null -eq $previousPort) { Remove-Item Env:OHOS_HDC_SERVER_PORT -ErrorAction SilentlyContinue } else { $env:OHOS_HDC_SERVER_PORT = $previousPort }
}
