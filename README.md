# sysmon

Monitor leve para host **Proxmox / Debian** com ícone e overlay na bandeja do **Windows**.

O agente Linux é **stdlib pura** — nenhum `pip install` no host do PVE, o que evita
brigar com o PEP 668 e com o Python de sistema do Proxmox. O cliente Windows é um
ícone de bandeja que muda de cor conforme a temperatura, mais um overlay arrastável.

```
┌────────────── host PVE ──────────────┐        ┌──── Windows ────┐
│  sysmon_agent.py  (systemd, sem root)│        │   traymon.py    │
│    lê /sys, /proc  ──►  GET /metrics │◄──HTTP─┤  tray + overlay │
│  sysmon-thinpool.timer ► lvs (root)  │  token │                 │
└──────────────────────────────────────┘        └─────────────────┘
```

## Estrutura

| Caminho | Onde roda | O que é |
|---|---|---|
| `linux-agent/sysmon_agent.py` | host PVE | agente HTTP read-only, stdlib pura |
| `linux-agent/sysmon-cli.py` | host PVE | leitor standalone no terminal (`--watch`) |
| `linux-agent/sysmon-agent.service` | host PVE | unit systemd com hardening |
| `linux-agent/sysmon-thinpool.{service,timer}` | host PVE | snapshot do thin pool LVM |
| `linux-agent/install.sh` | host PVE | instala, gera token e testa |
| `windows-tray/traymon.py` | Windows | ícone de bandeja + overlay |
| `windows-tray/config.example.json` | Windows | modelo de configuração |
| `windows-tray/traymon.vbs` | Windows | inicia sem janela de console |
| `windows-tray/instalar-autostart.ps1` | Windows | registra no Agendador de Tarefas |

## Instalação — host Proxmox

Pré-requisito: sensores expostos pelo kernel.

```bash
apt install lm-sensors
sensors-detect          # responda YES; grava os módulos em /etc/modules
sensors                 # confirme que aparecem temperaturas
```

Depois:

```bash
git clone https://github.com/SEU_USUARIO/sysmon.git
cd sysmon/linux-agent
sudo ./install.sh 10.0.0.5          # use o IP da LAN do PVE
```

O instalador copia para `/opt/sysmon`, gera o token em `/etc/sysmon/token.env`,
sobe o serviço, ativa o timer do thin pool se houver LVM thin, e faz um `curl`
de teste. No fim ele imprime a URL e o token para você colar no Windows.

Feche a porta para o resto da rede:

```bash
iptables -A INPUT -p tcp --dport 9109 -s <IP_DO_WINDOWS> -j ACCEPT
iptables -A INPUT -p tcp --dport 9109 -j DROP
```

## Instalação — Windows

```powershell
cd windows-tray
python -m pip install -r requirements.txt
copy config.example.json config.json
notepad config.json          # cole url e token
python traymon.py            # primeiro teste COM console, para ver erros
```

Funcionando, registre o autostart:

```powershell
powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
```

Ele testa o script antes de registrar e configura início 30s após o login,
com reinício automático em caso de queda. Se preferir algo mais simples,
arraste `traymon.vbs` para `shell:startup`.

Proteja o token para leitura só do seu usuário:

```powershell
icacls config.json /inheritance:r /grant:r "$env:USERNAME:R"
```

## Uso

O ícone da bandeja mostra a temperatura da CPU em número, colorido:

- **verde** abaixo de 75% do valor crítico do sensor
- **amarelo** entre 75% e 90%
- **vermelho** acima de 90%, com um ponto de alerta no canto

Os limiares são derivados do `crit` que o próprio sensor reporta (numa CPU com
crit de 100°C, amarelo começa em 75°C). Sensores sem `crit` caem no fallback
fixo de `aviso_c` / `critico_c` do config.

Menu do ícone: alternar overlay, cliques atravessam (o overlay vira decorativo e
não intercepta o mouse), atualizar agora, copiar JSON, sair.

## Endpoints

```
GET /metrics   -> telemetria completa   (exige token)
GET /health    -> {"ok": true}          (sem auth, para healthcheck)
```

Autenticação por `Authorization: Bearer <token>` ou `?token=<token>`.

```bash
curl -H "Authorization: Bearer $TOKEN" http://10.0.0.5:9109/metrics | jq
```

Campos principais: `cpu_temp`, `cpu_crit`, `temps[]`, `fans{}`, `load[]`,
`cpu_percent`, `mem{}`, `discos[]`, `thinpools[]`, `guests{}`, `uptime_s`.

## Segurança

Leia [docs/seguranca.md](docs/seguranca.md) antes de expor qualquer porta.
Resumo: o agente é read-only e sem `subprocess` por decisão de projeto,
roda sem root via `DynamicUser`, e **não deve ser exposto à internet** —
o HTTP é puro, o token viaja em texto claro. Para acesso remoto, use WireGuard
ou Tailscale e faça bind na interface do túnel.

## Solução de problemas

Consulte [docs/troubleshooting.md](docs/troubleshooting.md).

## Licença

MIT.
