# Remove o sysmon do autostart e encerra o que estiver rodando.
foreach ($t in @("sysmon", "traymon")) {
    Unregister-ScheduledTask -TaskName $t -Confirm:$false -ErrorAction SilentlyContinue
}
Get-CimInstance Win32_Process -Filter "Name like 'python%'" -ErrorAction SilentlyContinue |
    Where-Object { $_.CommandLine -like "*sysmon.pyz*" -or $_.CommandLine -like "*traymon.py*" } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Write-Host "sysmon removido do autostart."
