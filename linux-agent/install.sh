#!/usr/bin/env bash
# Instala o sysmon-agent neste host Linux (Debian, Ubuntu ou Proxmox).
#
#   sudo ./install.sh <IP_DE_BIND> [porta] [-y]
#
# Precisa do binario ao lado deste script. Gere com `make dist` na sua
# maquina e copie a pasta inteira para o host, ou use o deploy.sh, que faz
# isso por SSH em varios hosts de uma vez.
set -euo pipefail

DESTINO=/opt/sysmon
CONF=/etc/sysmon
UNIT=/etc/systemd/system/sysmon-agent.service
AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

vermelho() { printf '\033[31m%s\033[0m\n' "$*"; }
amarelo()  { printf '\033[33m%s\033[0m\n' "$*"; }
verde()    { printf '\033[32m%s\033[0m\n' "$*"; }

BIND_IP=""
PORT=9109
SIM=0
for arg in "$@"; do
    case "$arg" in
        -y|--yes) SIM=1 ;;
        *[!0-9]*) BIND_IP="$arg" ;;
        *)        PORT="$arg" ;;
    esac
done

confirmar() {  # confirmar "pergunta" -> 0 se sim
    [[ $SIM -eq 1 ]] && return 0
    read -rp "$1 [s/N] " r
    [[ "$r" =~ ^[sSyY]$ ]]
}

[[ $EUID -eq 0 ]] || { vermelho "Rode como root."; exit 1; }

# ---------------------------------------------------------------- validacoes
if [[ -z "$BIND_IP" ]]; then
    vermelho "Uso: sudo ./install.sh <IP_DE_BIND> [porta] [-y]"
    echo "IPs disponiveis nesta maquina:"
    ip -4 -brief addr show | awk '{print "  " $1 "\t" $3}'
    exit 1
fi

if [[ "$BIND_IP" == "0.0.0.0" || "$BIND_IP" == "::" ]]; then
    vermelho "Recusando bind em todas as interfaces."
    echo "O transporte e HTTP puro: o token viaja em texto claro."
    echo "Use o IP da LAN, ou o IP do tunel WireGuard/Tailscale."
    exit 1
fi

# Comparacao exata: com grep simples, 10.0.0.5 casaria com 10.0.0.50.
if ! ip -4 -brief addr show | awk '{for(i=3;i<=NF;i++){split($i,a,"/"); print a[1]}}' \
     | grep -qxF "$BIND_IP"; then
    amarelo "AVISO: $BIND_IP nao esta configurado em nenhuma interface desta maquina."
    confirmar "Continuar mesmo assim?" || exit 1
fi

# ---------------------------------------------------------------- binario
case "$(uname -m)" in
    x86_64)  ARCO=amd64 ;;
    aarch64) ARCO=arm64 ;;
    *)       ARCO="$(uname -m)" ;;
esac

BINARIO=""
for c in "$AQUI/bin/sysmon-agent" "$AQUI/sysmon-agent" \
         "$AQUI/bin/sysmon-agent-linux-$ARCO" "$AQUI/sysmon-agent-linux-$ARCO"; do
    [[ -f "$c" ]] && { BINARIO="$c"; break; }
done

if [[ -z "$BINARIO" ]] && command -v go >/dev/null; then
    echo "==> Binario nao encontrado, mas ha Go instalado. Compilando..."
    (cd "$AQUI" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
        -o "$AQUI/bin/sysmon-agent" .)
    BINARIO="$AQUI/bin/sysmon-agent"
fi

if [[ -z "$BINARIO" ]]; then
    vermelho "Nao achei o binario do agente para $ARCO."
    echo "Na sua maquina de desenvolvimento: cd linux-agent && make dist"
    echo "Depois copie bin/sysmon-agent-linux-$ARCO para este host."
    exit 1
fi

if ! "$BINARIO" --version >/dev/null 2>&1; then
    vermelho "O binario $BINARIO nao executa nesta maquina (arquitetura errada?)."
    exit 1
fi
VERSAO="$("$BINARIO" --version)"

# ---------------------------------------------------------------- instalacao
echo "==> Instalando sysmon-agent $VERSAO em $DESTINO"
install -d -m 755 "$DESTINO"
install -m 755 "$BINARIO" "$DESTINO/sysmon-agent"

echo "==> Token"
install -d -m 700 "$CONF"
if [[ -f "$CONF/token.env" ]]; then
    verde "    token.env ja existe, mantendo o token atual"
else
    if command -v openssl >/dev/null; then
        TOKEN="$(openssl rand -hex 24)"
    else
        TOKEN="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
    printf 'SYSMON_TOKEN=%s\n' "$TOKEN" > "$CONF/token.env"
    chmod 600 "$CONF/token.env"
    verde "    token novo gerado"
fi

# Proxmox tem /etc/pve; so ali faz sentido o grupo www-data e o thin pool LVM.
EH_PVE=0
[[ -d /etc/pve ]] && EH_PVE=1

echo "==> Units systemd"
sed -e "s|__BIND_IP__|$BIND_IP|" -e "s|__PORT__|$PORT|" \
    "$AQUI/sysmon-agent.service" > "$UNIT"
if [[ $EH_PVE -eq 0 ]]; then
    # Fora do Proxmox esse grupo nao serve para nada; menos acesso, melhor.
    sed -i '/^SupplementaryGroups=www-data$/d' "$UNIT"
fi
install -m 644 "$AQUI/sysmon-thinpool.service" /etc/systemd/system/
install -m 644 "$AQUI/sysmon-thinpool.timer"   /etc/systemd/system/
if [[ -f "$AQUI/sysmon-smart.sh" ]]; then
    install -m 755 "$AQUI/sysmon-smart.sh" "$DESTINO/"
    install -m 644 "$AQUI/sysmon-smart.service" /etc/systemd/system/
    install -m 644 "$AQUI/sysmon-smart.timer"   /etc/systemd/system/
fi

systemctl daemon-reload
systemctl enable --now sysmon-agent.service

# Thin pool so existe onde ha LVM thin. ZFS e ext4 puro nao precisam.
if command -v lvs >/dev/null && \
   lvs --noheadings --select 'lv_attr=~^t' 2>/dev/null | grep -q .; then
    systemctl enable --now sysmon-thinpool.timer
    verde "    thin pool LVM detectado, timer ativado"
else
    echo "    sem thin pool LVM, timer nao ativado"
fi

# SMART precisa de root, entao roda num timer isolado - o agente exposto a
# rede continua sem executar nada. Sem smartmontools, o campo vem null.
if [[ -f /etc/systemd/system/sysmon-smart.timer ]]; then
    if command -v smartctl >/dev/null; then
        systemctl enable --now sysmon-smart.timer
        systemctl start sysmon-smart.service 2>/dev/null || true
        verde "    smartctl encontrado, coleta de SMART ativada"
    else
        echo "    smartmontools nao instalado; desgaste e horas dos discos ficam"
        echo "    indisponiveis. Para ativar: apt install smartmontools &&"
        echo "    systemctl enable --now sysmon-smart.timer"
    fi
fi

echo "==> Sensores"
if ! ls /sys/class/hwmon/hwmon*/temp*_input >/dev/null 2>&1; then
    amarelo "    nenhum sensor de temperatura exposto pelo kernel."
    echo "    Rode: apt install lm-sensors && sensors-detect"
    echo "    (o agente funciona sem isso; a temperatura vem null)"
else
    verde "    $(ls /sys/class/hwmon/hwmon*/temp*_input 2>/dev/null | wc -l) sensores encontrados"
fi

# drivetemp expoe a temperatura de discos SATA; NVMe ja aparece sem modulo.
if ls /sys/block/sd* >/dev/null 2>&1 && ! lsmod 2>/dev/null | grep -q '^drivetemp'; then
    if modprobe drivetemp 2>/dev/null; then
        echo "drivetemp" > /etc/modules-load.d/sysmon-drivetemp.conf
        verde "    drivetemp carregado (temperatura dos discos SATA)"
    else
        echo "    sem drivetemp: discos SATA ficam sem temperatura (normal em kernel antigo)"
    fi
fi

# ---------------------------------------------------------------- teste
echo
echo "==> Teste"
TOKEN="$(sed -n 's/^SYSMON_TOKEN=//p' "$CONF/token.env")"

ok=0
for _ in $(seq 1 10); do
    if curl -sf -m 3 "http://$BIND_IP:$PORT/health" >/dev/null 2>&1; then
        ok=1; break
    fi
    sleep 1
done

if [[ $ok -eq 0 ]]; then
    vermelho "    FALHOU: o agente nao respondeu no /health."
    echo "    Veja: journalctl -u sysmon-agent -n 30 --no-pager"
    exit 1
fi

if curl -sf -m 5 -H "Authorization: Bearer $TOKEN" \
        "http://$BIND_IP:$PORT/metrics" >/dev/null; then
    verde "    OK - agente respondendo em http://$BIND_IP:$PORT/metrics"
else
    vermelho "    /health respondeu mas /metrics recusou o token."
    echo "    Veja: journalctl -u sysmon-agent -n 30 --no-pager"
    exit 1
fi

echo
verde "Pronto. Para o cliente:"
echo "  url   : http://$BIND_IP:$PORT/metrics"
echo "  token : $TOKEN"
echo
amarelo "Nao esqueca do firewall - libere so as maquinas que vao monitorar:"
if command -v nft >/dev/null; then
    echo "  nft add rule inet filter input tcp dport $PORT ip saddr != <IP_CLIENTE> drop"
fi
echo "  iptables -A INPUT -p tcp --dport $PORT -s <IP_CLIENTE> -j ACCEPT"
echo "  iptables -A INPUT -p tcp --dport $PORT -j DROP"
