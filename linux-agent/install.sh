#!/usr/bin/env bash
# Instala o sysmon-agent no host Debian/Proxmox.
#   sudo ./install.sh 10.0.0.5 [porta]
set -euo pipefail

BIND_IP="${1:-}"
PORT="${2:-9109}"
DESTINO=/opt/sysmon
CONF=/etc/sysmon

vermelho() { printf '\033[31m%s\033[0m\n' "$*"; }
verde()    { printf '\033[32m%s\033[0m\n' "$*"; }

[[ $EUID -eq 0 ]] || { vermelho "Rode como root."; exit 1; }

if [[ -z "$BIND_IP" ]]; then
    vermelho "Uso: sudo ./install.sh <IP_DA_LAN> [porta]"
    echo "IPs disponiveis nesta maquina:"
    ip -4 -brief addr show | awk '{print "  " $1 "\t" $3}'
    exit 1
fi

if [[ "$BIND_IP" == "0.0.0.0" ]]; then
    vermelho "Recusando bind em 0.0.0.0. Use o IP da LAN ou do tunel VPN."
    exit 1
fi

if ! ip -4 -brief addr show | grep -q "$BIND_IP"; then
    vermelho "AVISO: $BIND_IP nao aparece nas interfaces desta maquina."
    read -rp "Continuar mesmo assim? [s/N] " r
    [[ "$r" =~ ^[sS]$ ]] || exit 1
fi

echo "==> Instalando arquivos"
install -d -m 755 "$DESTINO"
install -m 755 sysmon_agent.py "$DESTINO/"
install -m 755 sysmon-cli.py   "$DESTINO/"

echo "==> Gerando token"
install -d -m 700 "$CONF"
if [[ -f "$CONF/token.env" ]]; then
    verde "    token.env ja existe, mantendo o token atual"
else
    TOKEN="$(openssl rand -hex 24)"
    printf 'SYSMON_TOKEN=%s\n' "$TOKEN" > "$CONF/token.env"
    chmod 600 "$CONF/token.env"
fi

echo "==> Instalando units systemd"
sed -e "s|__BIND_IP__|$BIND_IP|" -e "s|__PORT__|$PORT|" \
    sysmon-agent.service > /etc/systemd/system/sysmon-agent.service
install -m 644 sysmon-thinpool.service /etc/systemd/system/
install -m 644 sysmon-thinpool.timer   /etc/systemd/system/

systemctl daemon-reload
systemctl enable --now sysmon-agent.service

# thin pool so faz sentido se houver LVM thin nesta maquina
if command -v lvs >/dev/null && lvs --noheadings --select 'lv_attr=~^t' 2>/dev/null | grep -q .; then
    systemctl enable --now sysmon-thinpool.timer
    verde "    thin pool LVM detectado, timer ativado"
else
    echo "    sem thin pool LVM, timer nao ativado (ZFS/ext4 puro)"
fi

echo "==> Sensores"
if ! command -v sensors >/dev/null; then
    vermelho "    lm-sensors nao instalado. Rode: apt install lm-sensors && sensors-detect"
elif ! ls /sys/class/hwmon/hwmon*/temp*_input >/dev/null 2>&1; then
    vermelho "    nenhum sensor exposto. Rode: sensors-detect"
else
    verde "    $(ls /sys/class/hwmon/hwmon*/temp*_input 2>/dev/null | wc -l) sensores encontrados"
fi

sleep 1
echo
echo "==> Teste"
TOKEN="$(grep -oP '(?<=SYSMON_TOKEN=).*' "$CONF/token.env")"
if curl -sf -m 5 -H "Authorization: Bearer $TOKEN" \
        "http://$BIND_IP:$PORT/metrics" >/dev/null; then
    verde "    OK - agente respondendo"
else
    vermelho "    FALHOU. Veja: journalctl -u sysmon-agent -n 30"
    exit 1
fi

echo
verde "Pronto. Configure o cliente Windows com:"
echo "  url   : http://$BIND_IP:$PORT/metrics"
echo "  token : $TOKEN"
echo
echo "Nao esqueca do firewall. Exemplo liberando so o seu desktop:"
echo "  iptables -A INPUT -p tcp --dport $PORT -s <IP_DO_WINDOWS> -j ACCEPT"
echo "  iptables -A INPUT -p tcp --dport $PORT -j DROP"
