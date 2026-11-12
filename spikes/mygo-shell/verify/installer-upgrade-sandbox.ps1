# Runs inside Windows Sandbox (user session). Drives the NSIS installer GUI with UI Automation. Phase 1-3a.
$out = 'C:\Users\WDAGUtilityAccount\Desktop\out'
$setup = 'C:\Users\WDAGUtilityAccount\Desktop\in\GuiZhi-Setup-0.28.0-x64.exe'
Remove-Item "$out\GUI3.flag" -Force -ErrorAction SilentlyContinue
Start-Transcript -Path "$out\gui3.log" -Force | Out-Null
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



$install = 'C:\Users\WDAGUtilityAccount\AppData\Local\Programs\GuiZhi'
$marker = "$env:APPDATA\GuiZhi\.machine\legacy-localstorage-imported"
function Send-To($w, [string]$keys) {
  try { $w.SetFocus() } catch {}
  try { [Microsoft.VisualBasic.Interaction]::AppActivate($w.Current.ProcessId) } catch {}
  Start-Sleep -Milliseconds 400
  [System.Windows.Forms.SendKeys]::SendWait($keys)
}
"##### baseline"; State 'baseline'
"  LS dir exists = $(Test-Path "$env:APPDATA\GuiZhi\Local Storage\leveldb")  marker exists = $(Test-Path $marker)"

"##### U1: 升级安装（确定备份提示后一路回车）"
$p = Start-Process $setup -PassThru
$w = @(Wait-Window 40)
Start-Sleep -Milliseconds 800
Send-To $w[0] '{ENTER}'
for ($i = 1; $i -le 14 -and -not $p.HasExited; $i++) {
  Start-Sleep -Seconds 4
  $ws = @(Get-InstallerWindows)
  "  step $i  alive=$(-not $p.HasExited)"
  if ($ws.Count -gt 0 -and -not $p.HasExited) { Send-To $ws[0] '{ENTER}' }
}
"  installer exit code = $(if ($p.HasExited) { $p.ExitCode } else { 'still running' })"
State 'after upgrade'

"##### U2: 首次启动（安装器完成页自动拉起）"
$t0 = Get-Date
while (((Get-Date) - $t0).TotalSeconds -lt 40 -and -not (Get-Process GuiZhi -ErrorAction SilentlyContinue)) { Start-Sleep 1 }
Start-Sleep 12
"  GuiZhi processes: " + ((Get-Process GuiZhi -ErrorAction SilentlyContinue | ForEach-Object { "#$($_.Id)" }) -join ' ')
"  window title: " + ((Get-Process GuiZhi -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowTitle } | ForEach-Object { "'$($_.MainWindowTitle)'" }) -join ' ')
Shot 'gui3-launch1'
"  marker exists after first launch = $(Test-Path $marker)"
$el = "$env:APPDATA\GuiZhi\logs\error.log"
if (Test-Path $el) { "  error.log lines = " + @(Get-Content $el).Count }

"##### U3: 关闭后再次启动（localStorage 必须保留，不再导入）"
Get-Process GuiZhi -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep 5
Start-Process "$install\GuiZhi.exe"
Start-Sleep 14
"  GuiZhi processes: " + ((Get-Process GuiZhi -ErrorAction SilentlyContinue | ForEach-Object { "#$($_.Id)" }) -join ' ')
Shot 'gui3-launch2'
"  marker still exists = $(Test-Path $marker)"
Stop-Transcript | Out-Null
New-Item -ItemType File -Path "$out\GUI3.flag" -Force | Out-Null