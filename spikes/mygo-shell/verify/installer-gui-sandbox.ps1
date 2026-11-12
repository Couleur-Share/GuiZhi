# Runs inside Windows Sandbox (user session). Drives the NSIS installer GUI with UI Automation. Phase 1-3a.
$out = 'C:\Users\WDAGUtilityAccount\Desktop\out'
$setup = 'C:\Users\WDAGUtilityAccount\Desktop\in\GuiZhi-Setup-0.28.0-x64.exe'
Remove-Item "$out\GUI2.flag" -Force -ErrorAction SilentlyContinue
Start-Transcript -Path "$out\gui2.log" -Force | Out-Null
Add-Type -AssemblyName Microsoft.VisualBasic
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Windows.Forms, System.Drawing
Add-Type @'
using System.Runtime.InteropServices;
public class Dpi { [DllImport("user32.dll")] public static extern bool SetProcessDPIAware(); }
'@
[Dpi]::SetProcessDPIAware() | Out-Null
$AE = [System.Windows.Automation.AutomationElement]
$TS = [System.Windows.Automation.TreeScope]
function Shot($name) {
  $b = [System.Windows.Forms.SystemInformation]::VirtualScreen
  $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
  $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size)
  $bmp.Save("$out\$name.png", [System.Drawing.Imaging.ImageFormat]::Png); $g.Dispose(); $bmp.Dispose()
}
function Get-InstallerWindows {
  $pids = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'GuiZhi-Setup*' -or $_.Name -like 'nsc*' -or $_.Name -like 'Uninstall*' } | ForEach-Object Id)
  $wins = $AE::RootElement.FindAll($TS::Children, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($w in $wins) { if ($pids -contains $w.Current.ProcessId) { $w } }
}
function Describe($w) {
  "  WINDOW title='$($w.Current.Name)' class=$($w.Current.ClassName) pid=$($w.Current.ProcessId)"
  $all = $w.FindAll($TS::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($e in $all) {
    $t = $e.Current.ControlType.ProgrammaticName -replace 'ControlType\.', ''
    if ($t -in 'Text', 'Button', 'CheckBox', 'RadioButton', 'Edit', 'ComboBox') {
      $extra = ''
      if ($t -in 'CheckBox', 'RadioButton') { try { $extra = " state=" + $e.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Current.ToggleState } catch { try { $extra = " selected=" + $e.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Current.IsSelected } catch {} } }
      if ($t -eq 'Edit') { try { $extra = " value=" + $e.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern).Current.Value } catch {} }
      "    [$t] '$($e.Current.Name)'$extra"
    }
  }
}
function Click-Button($w, [string]$regex) {
  $btns = $w.FindAll($TS::Descendants, (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)))
  foreach ($b in $btns) {
    if ($b.Current.Name -match $regex) {
      try { $b.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); "  -> clicked '$($b.Current.Name)'"; return $true } catch { "  !! invoke failed for '$($b.Current.Name)': $_" }
    }
  }
  "  !! no button matching /$regex/"; return $false
}
function Wait-Window([int]$timeoutSec) {
  $t0 = Get-Date
  while (((Get-Date) - $t0).TotalSeconds -lt $timeoutSec) {
    $w = @(Get-InstallerWindows | Where-Object { $_.Current.Name -ne '' -or $_.Current.ClassName -eq '#32770' })
    if ($w.Count) { return $w }
    Start-Sleep -Milliseconds 500
  }
  return @()
}
function State($label) {
  $reg = 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\fa5aa350-68bd-43b9-b440-a7ecb8fcc78a'
  $dn = if (Test-Path $reg) { (Get-ItemProperty $reg).DisplayName } else { '(none)' }
  $exe = 'C:\Users\WDAGUtilityAccount\AppData\Local\Programs\GuiZhi\GuiZhi.exe'
  $sz = if (Test-Path $exe) { [int]((Get-Item $exe).Length / 1MB) } else { 0 }
  "  STATE[$label] uninstall displayName='$dn' GuiZhi.exe=${sz}MB electronResourcesAsar=$(Test-Path 'C:\Users\WDAGUtilityAccount\AppData\Local\Programs\GuiZhi\resources\app.asar') dataDb=$(Test-Path "$env:APPDATA\GuiZhi\data\knowledge.db")"
}



$KEY = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$BASE = 'C:\Program Files (x86)\Microsoft\EdgeWebView\Application'
$real = (Get-ChildItem $BASE -Directory | Where-Object { Test-Path "$($_.FullName)\msedgewebview2.exe" } | Select-Object -First 1).FullName
$stale = "$BASE\157.0.4341.42"
function Set-Registry([string]$v) { Set-ItemProperty -Path $KEY -Name EBWebView -Value $v; "  registry EBWebView -> $v" }

# 把键盘输入送给安装程序的对话框：先激活，再 SendWait。
function Send-To($w, [string]$keys) {
  $procId = $w.Current.ProcessId
  try { $w.SetFocus() } catch {}
  try { [Microsoft.VisualBasic.Interaction]::AppActivate($procId) } catch {}
  Start-Sleep -Milliseconds 400
  [System.Windows.Forms.SendKeys]::SendWait($keys)
  "  -> sent keys '$keys' to pid $procId"
}
function Run-Dialog([string]$label, [string]$keys, [string]$shot) {
  "##### $label"
  $p = Start-Process $setup -PassThru
  $w = @(Wait-Window 40)
  "windows found: $($w.Count)"
  if ($w.Count -eq 0) { "  !! no window"; if (-not $p.HasExited) { $p | Stop-Process -Force }; return $null }
  Start-Sleep -Milliseconds 800
  Shot $shot
  Send-To $w[0] $keys | Out-Null
  "  sent keys '$keys'"
  $null = $p.WaitForExit(20000)
  if ($p.HasExited) { "  installer exited, exit code = $($p.ExitCode)" } else { "  !! installer still running after keys; killing"; $p | Stop-Process -Force }
  return $p
}

"##### baseline"; State 'baseline'

# ---- WebView2 对话框（注册表登记目录与磁盘不一致）----
Set-Registry $stale | Out-Null
$null = Run-Dialog 'W1: WebView2 dialog -> press N (否)' 'n' 'gui2-w1'
State 'after W1 (No)'
"  Edge/browser opened? " + (@(Get-Process msedge -ErrorAction SilentlyContinue).Count) + " msedge procs before W2"
$before = @(Get-Process msedge -ErrorAction SilentlyContinue | ForEach-Object Id)
$null = Run-Dialog 'W2: WebView2 dialog -> press Y (是)' 'y' 'gui2-w2'
Start-Sleep 4
$after = @(Get-CimInstance Win32_Process -Filter "Name='msedge.exe'" | Where-Object { $before -notcontains $_.ProcessId })
"  new msedge processes after Yes: $($after.Count)"
$after | Where-Object { $_.CommandLine -match 'microsoft\.com' } | Select-Object -First 2 | ForEach-Object { "  cmd: $($_.CommandLine.Substring(0, [Math]::Min(200, $_.CommandLine.Length)))" }
Shot 'gui2-w2-after-yes'
Get-Process msedge -ErrorAction SilentlyContinue | Where-Object { $before -notcontains $_.Id } | Stop-Process -Force -ErrorAction SilentlyContinue
State 'after W2 (Yes)'

# ---- 备份提示 ----
Set-Registry $real | Out-Null
$null = Run-Dialog 'B1: backup prompt -> press Esc (取消)' '{ESC}' 'gui2-b1'
State 'after B1 (Cancel)'

# ---- 确定后走完整个向导 ----
"##### B2: backup prompt -> Enter (确定), then walk the wizard with Enter"
$p = Start-Process $setup -PassThru
$w = @(Wait-Window 40)
Start-Sleep -Milliseconds 800
Shot 'gui2-b2-prompt'
Send-To $w[0] '{ENTER}' | Out-Null
for ($i = 1; $i -le 14 -and -not $p.HasExited; $i++) {
  Start-Sleep -Seconds 4
  $ws = @(Get-InstallerWindows)
  $titles = ($ws | ForEach-Object { "'$($_.Current.Name)'#$($_.Current.ProcessId)" }) -join ' '
  "  step $i  alive=$(-not $p.HasExited)  windows: $titles"
  Shot ("gui2-b2-step{0:00}" -f $i)
  if ($ws.Count -gt 0 -and -not $p.HasExited) { Send-To $ws[0] '{ENTER}' | Out-Null }
}
if ($p.HasExited) { "  installer exited, exit code = $($p.ExitCode)" } else { "  !! installer still running after wizard loop" }
State 'after wizard'
Start-Sleep 20
"  GuiZhi processes: " + ((Get-Process GuiZhi, msedgewebview2 -ErrorAction SilentlyContinue | ForEach-Object { "$($_.Name)#$($_.Id)" }) -join ' ')
"  GuiZhi windows: " + ((Get-Process GuiZhi -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowTitle } | ForEach-Object { "'$($_.MainWindowTitle)'" }) -join ' ')
Shot 'gui2-app-after-install'
$el = "$env:APPDATA\GuiZhi\logs\error.log"
"  error.log exists = $(Test-Path $el)"
if (Test-Path $el) { Get-Content $el -Tail 5 -Encoding UTF8 }
Stop-Transcript | Out-Null
New-Item -ItemType File -Path "$out\GUI2.flag" -Force | Out-Null

