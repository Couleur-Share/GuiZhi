# Runs inside Windows Sandbox (user session). Drives the NSIS installer GUI with UI Automation. Phase 1-3a.
$out = 'C:\Users\WDAGUtilityAccount\Desktop\out'
$setup = 'C:\Users\WDAGUtilityAccount\Desktop\in\GuiZhi-Setup-0.28.0-x64.exe'
Remove-Item "$out\GUI9.flag" -Force -ErrorAction SilentlyContinue
Start-Transcript -Path "$out\gui9.log" -Force | Out-Null
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
$GUIDS = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', '{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}', '{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}', '{65C35B14-6C1D-4122-AC46-7148CC9D6497}'
$REGKEYS = foreach ($g in $GUIDS) {
  "HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\$g", "HKLM\SOFTWARE\Microsoft\EdgeUpdate\ClientState\$g", "HKCU\Software\Microsoft\EdgeUpdate\ClientState\$g",
  "HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$g", "HKLM\SOFTWARE\Microsoft\EdgeUpdate\Clients\$g", "HKCU\Software\Microsoft\EdgeUpdate\Clients\$g"
}
function Send-To($w, [string]$keys) {
  try { $w.SetFocus() } catch {}
  try { [Microsoft.VisualBasic.Interaction]::AppActivate($w.Current.ProcessId) } catch {}
  Start-Sleep -Milliseconds 400
  [System.Windows.Forms.SendKeys]::SendWait($keys)
}
function Hide-WebView2 {
  $bak = "$env:TEMP\regbak"; New-Item -ItemType Directory -Force $bak | Out-Null; $i = 0
  foreach ($k in $REGKEYS) {
    $psk = $k -replace '^HKLM\\', 'HKLM:\' -replace '^HKCU\\', 'HKCU:\'
    if (Test-Path $psk) { $i++; reg export $k "$bak\k$i.reg" /y | Out-Null; reg delete $k /f | Out-Null; "  hid $k" }
  }
}
function Restore-WebView2 { Get-ChildItem "$env:TEMP\regbak\*.reg" -ErrorAction SilentlyContinue | ForEach-Object { reg import $_.FullName 2>&1 | Out-Null }; "  registry restored" }
function Wait-Gone($p, [int]$sec) { $null = $p.WaitForExit($sec * 1000); if ($p.HasExited) { "  exited, code=$($p.ExitCode)" } else { "  !! still running; killing pid $($p.Id)"; Stop-Process -Id $p.Id -Force } }

function Stop-App { Get-Process GuiZhi, msedgewebview2 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep 3 }
Add-Type @"
using System.Runtime.InteropServices;
public class Mouse {
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(int flags, int dx, int dy, int data, int extra);
}
"@
function ClickRel($w, [double]$fx, [double]$fy) {
  $r = $w.Current.BoundingRectangle
  $x = [int]($r.X + $r.Width * $fx); $y = [int]($r.Y + $r.Height * $fy)
  [Mouse]::SetCursorPos($x, $y); Start-Sleep -Milliseconds 300
  [Mouse]::mouse_event(2, 0, 0, 0, 0); Start-Sleep -Milliseconds 80; [Mouse]::mouse_event(4, 0, 0, 0, 0)
  "  -> click at ($x,$y) in window rect ($([int]$r.X),$([int]$r.Y),$([int]$r.Width)x$([int]$r.Height))"
}
function Press($keys) {
  $wsh = New-Object -ComObject WScript.Shell
  $ok = $wsh.AppActivate('GuiZhi 安装'); Start-Sleep -Milliseconds 600
  [System.Windows.Forms.SendKeys]::SendWait($keys)
  "  -> AppActivate=$ok, sent $keys"
}
Get-Process | Where-Object { $_.Name -match 'Setup|nsc|MOTW' } | Stop-Process -Force -ErrorAction SilentlyContinue
Stop-App
State 'baseline (应为 Electron)'
"##### G4: 从 Electron 版经应用内更新方式（--updated --force-run，无 /S）切到 Go 壳"
$app = Start-Process "$install\GuiZhi.exe" -PassThru; Start-Sleep 14
"  old app up: $(-not $app.HasExited)"
$inst = Start-Process $setup -ArgumentList '--updated', '--force-run' -PassThru
Start-Sleep 1; Stop-Process -Id $app.Id -Force -ErrorAction SilentlyContinue
$sent = $false
for ($i = 1; $i -le 20 -and -not $inst.HasExited; $i++) {
  Start-Sleep 4
  $ws = @(Get-InstallerWindows)
  "  step $i  alive=$(-not $inst.HasExited)  titles=[$(($ws | ForEach-Object { $_.Current.Name }) -join '|')]"
  Shot ("gui9-{0:00}" -f $i)
  if ($ws.Count -and ($i -eq 3 -or $i -eq 5)) { ClickRel $ws[0] 0.73 0.94 }
}
"  installer exited=$($inst.HasExited) code=$(if ($inst.HasExited) { $inst.ExitCode } else { 'n/a' })"
if (-not $inst.HasExited) { Stop-Process -Id $inst.Id -Force }
Start-Sleep 15
"  GuiZhi after: " + ((Get-Process GuiZhi -ErrorAction SilentlyContinue | ForEach-Object { "#$($_.Id) title='$($_.MainWindowTitle)'" }) -join ' ')
Shot 'gui9-final'
State 'after G4'
Stop-Transcript | Out-Null
New-Item -ItemType File -Path "$out\GUI9.flag" -Force | Out-Null