#!/usr/bin/env bash
# Remove completamente o sysmon-agent.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "Rode como root."; exit 1; }

systemctl disable --now sysmon-agent.service sysmon-thinpool.timer 2>/dev/null || true
rm -f /etc/systemd/system/sysmon-agent.service \
      /etc/systemd/system/sysmon-thinpool.service \
      /etc/systemd/system/sysmon-thinpool.timer
systemctl daemon-reload
rm -rf /opt/sysmon /run/sysmon

read -rp "Remover tambem o token em /etc/sysmon? [s/N] " r
[[ "$r" =~ ^[sS]$ ]] && rm -rf /etc/sysmon

echo "Removido."
