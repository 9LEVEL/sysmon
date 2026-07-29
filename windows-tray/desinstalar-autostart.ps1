Unregister-ScheduledTask -TaskName "traymon" -Confirm:$false -ErrorAction SilentlyContinue
Get-Process pythonw -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -like "*python*" } | Stop-Process -Force -ErrorAction SilentlyContinue
Write-Host "traymon removido do autostart."
