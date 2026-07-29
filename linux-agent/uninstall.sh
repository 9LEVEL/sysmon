#!/usr/bin/env bash
# Remove completamente o sysmon-agent deste host.
#   sudo ./uninstall.sh [-y]
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "Rode como root."; exit 1; }

SIM=0
[[ "${1:-}" == "-y" || "${1:-}" == "--yes" ]] && SIM=1

systemctl disable --now sysmon-agent.service sysmon-thinpool.timer sysmon-smart.timer 2>/dev/null || true
rm -f /etc/systemd/system/sysmon-agent.service \
      /etc/systemd/system/sysmon-thinpool.service \
      /etc/systemd/system/sysmon-thinpool.timer \
      /etc/systemd/system/sysmon-smart.service \
      /etc/systemd/system/sysmon-smart.timer \
      /etc/modules-load.d/sysmon-drivetemp.conf
systemctl daemon-reload
systemctl reset-failed sysmon-agent.service 2>/dev/null || true
rm -rf /opt/sysmon /run/sysmon

if [[ $SIM -eq 1 ]]; then
    rm -rf /etc/sysmon
    echo "Removido (inclusive o token)."
    exit 0
fi

# Manter o token facilita reinstalar sem reconfigurar os clientes.
read -rp "Remover tambem o token em /etc/sysmon? [s/N] " r
[[ "$r" =~ ^[sS]$ ]] && rm -rf /etc/sysmon

echo "Removido."
