# sysmon

Monitor leve para **vários hosts Linux** (Proxmox, Debian, Ubuntu), com ícone e
overlay na bandeja do **Windows** e um dashboard no terminal.

O agente é um **binário Go estático**: instalar num host novo é copiar um
arquivo. Não depende do Python da distribuição, não usa `pip`, não executa
comando externo e roda sem root. Os clientes são Python stdlib puro.

```
┌── pve (Proxmox) ───┐  ┌── nas (Debian) ────┐  ┌── vps (Ubuntu) ────┐
│  sysmon-agent (Go) │  │  sysmon-agent (Go) │  │  sysmon-agent (Go) │
│  systemd, sem root │  │  systemd, sem root │  │  systemd, sem root │
│  lê /sys e /proc   │  │                    │  │                    │
└─────────┬──────────┘  └─────────┬──────────┘  └─────────┬──────────┘
          │                       │                       │
          └───────── GET /metrics + Bearer token ─────────┘
                                  │
                 ┌────────────────┴─────────────────┐
        ┌────────┴─────────┐              ┌─────────┴────────┐
        │   traymon.py     │              │  sysmon-dash.py  │
        │  bandeja Windows │              │ terminal (Linux) │
        └──────────────────┘              └──────────────────┘
```

## Estrutura

| Caminho | Onde roda | O que é |
|---|---|---|
| `linux-agent/*.go` | cada host | agente HTTP read-only, binário estático |
| `linux-agent/install.sh` | cada host | instala, gera token e testa |
| `linux-agent/deploy.sh` | sua máquina | instala em N hosts por SSH e gera o config |
| `linux-agent/sysmon-agent.service` | cada host | unit systemd com hardening + watchdog |
| `linux-agent/sysmon-thinpool.{service,timer}` | Proxmox | snapshot do thin pool LVM |
| `tools/sysmon_nucleo.py` | clientes | config, polling e regras de alerta (compartilhado) |
| `tools/sysmon-dash.py` | sua máquina | dashboard de N hosts no terminal |
| `tools/sysmon-cli.py` | um host | leitor local de sensores, sem rede nem token |
| `windows-tray/traymon.py` | Windows | ícone de bandeja + overlay multi-host |
| `windows-tray/instalar-autostart.ps1` | Windows | registra no Agendador de Tarefas |

## Instalação nos hosts Linux

### Pacote pronto (sem compilar nada)

Os releases trazem o agente empacotado: binário estático, units e instalador.
Não precisa de Go, Python nem internet no host de destino.

**Este repositório é privado**, então os assets do release exigem token — não
existe download anônimo. O caminho mais simples é baixar na sua máquina, onde
você já está autenticado, e mandar por `scp`:

```bash
# na sua máquina
gh release download v2.0.0-rc1 -p 'sysmon-agent-2.0.0-linux-amd64.tar.gz'
scp sysmon-agent-2.0.0-linux-amd64.tar.gz root@192.168.0.10:/tmp/

# no host
cd /tmp && tar xzf sysmon-agent-2.0.0-linux-amd64.tar.gz
cd sysmon-agent-2.0.0-linux-amd64
sudo ./install.sh 192.168.0.10          # IP da LAN ou do túnel
```

Para puxar direto do host, é preciso um token com leitura neste repositório.
A URL de browser (`/releases/download/...`) devolve 404 mesmo com token; use o
endpoint de API do asset:

```bash
# no host, com GH_TOKEN exportado
ASSET=$(curl -s -H "Authorization: Bearer $GH_TOKEN" \
  https://api.github.com/repos/9LEVEL/sysmon/releases/tags/v2.0.0-rc1 \
  | grep -B2 '"name": "sysmon-agent-2.0.0-linux-amd64.tar.gz"' \
  | grep '"id":' | head -1 | tr -dc 0-9)

curl -fL -H "Authorization: Bearer $GH_TOKEN" \
     -H "Accept: application/octet-stream" \
     -o sysmon-agent.tar.gz \
     "https://api.github.com/repos/9LEVEL/sysmon/releases/assets/$ASSET"
```

Se o repositório se tornar público, o `curl -fLO` na URL de browser passa a
funcionar sem nada disso.

Os clientes vêm em `sysmon-clientes-<versão>.zip` (Windows) ou `.tar.gz`.
Confira as somas com o `SHA256SUMS` do release.

Para gerar os pacotes você mesmo, sem passar pelo GitHub: `make pacote` (roda a
suite de testes antes). E note que o **`deploy.sh` já faz tudo isso por SSH** —
compila, copia e instala em N hosts, sem release nem token no meio.

### Vários hosts de uma vez (recomendado)

Na sua máquina, com Go e acesso SSH por chave aos hosts:

```bash
cd linux-agent
cat > hosts.conf <<'EOF'
# apelido  destino_ssh          ip_de_bind     porta
pve        root@192.168.0.10    192.168.0.10   9109
nas        root@192.168.0.20    192.168.0.20
vps        root@100.90.1.5      100.90.1.5            # IP do Tailscale
EOF

./deploy.sh hosts.conf
```

Ele compila, copia o binário certo para a arquitetura de cada host, instala,
testa e no fim escreve um `hosts.json` (modo 600) com os tokens de todos —
pronto para os dois clientes. É o passo que antes você fazia à mão.

### Um host só

```bash
cd linux-agent
make dist                      # gera bin/sysmon-agent-linux-{amd64,arm64}
scp -r ../linux-agent root@192.168.0.10:/tmp/
ssh root@192.168.0.10 '/tmp/linux-agent/install.sh 192.168.0.10'
```

O instalador copia para `/opt/sysmon`, gera o token em `/etc/sysmon/token.env`,
sobe o serviço, ativa o timer do thin pool **se** houver LVM thin, remove o
grupo `www-data` da unit **se** não for Proxmox, e testa. No fim imprime a URL
e o token.

Temperaturas exigem que o kernel exponha os sensores. O agente funciona sem
isso (`cpu_temp` vem `null`), mas para ter o número:

```bash
apt install lm-sensors
sensors-detect          # responda YES; grava os módulos em /etc/modules
sensors
```

Feche a porta para o resto da rede:

```bash
iptables -A INPUT -p tcp --dport 9109 -s <IP_DO_CLIENTE> -j ACCEPT
iptables -A INPUT -p tcp --dport 9109 -j DROP
```

## Dashboard no terminal

Roda de qualquer máquina Linux, inclusive por SSH. Só precisa do Python 3.9+.

```bash
python3 tools/sysmon-dash.py --config hosts.json
```

```
sysmon  3 host(s)  1 alerta(s)                                    14:32:05

HOST       CPU    TEMP   RAM    SWAP   DISCO             LOAD              REDE               PSI-IO  UP
pve        12%    47C    38%    2%     / 62%             0.40 0.30 0.20    v1.2M/s ^340K/s    0%      12d3h
nas        4%     39C    22%    --     /tank 91%         0.10 0.00 0.00    v12K/s ^8K/s       3%      45d0h
vps        offline: sem conexao (timed out)

! nas: disco /tank em 91%
! vps: sem conexao (timed out)
```

A tabela descarta colunas conforme o terminal estreita. Outras saídas:

```bash
python3 tools/sysmon-dash.py --once          # imprime e sai (útil em script)
python3 tools/sysmon-dash.py --host pve      # detalhe completo de um host
python3 tools/sysmon-dash.py --json          # a frota inteira em JSON
```

`--once` e `--host` saem com código 1 quando há host crítico ou offline, então
dá para usar direto em cron ou health check.

## Instalação — Windows

```powershell
git clone https://github.com/SEU_USUARIO/sysmon.git
cd sysmon\windows-tray
python -m pip install -r requirements.txt
copy ..\hosts.json config.json        # gerado pelo deploy.sh
python traymon.py                     # primeiro teste COM console, para ver erros
```

Sem o `deploy.sh`, copie `config.example.json` para `config.json` e preencha.

Funcionando, registre o autostart:

```powershell
powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
```

Proteja o arquivo, que contém os tokens de todos os hosts:

```powershell
icacls config.json /inheritance:r /grant:r "$env:USERNAME:R"
```

O `traymon.py` importa `tools/sysmon_nucleo.py` do repositório. Se preferir
copiar só a pasta `windows-tray`, leve o `sysmon_nucleo.py` junto para dentro
dela — o script procura nos dois lugares.

## Uso do tray

O ícone mostra a temperatura do **host mais quente** da frota, colorido pelo
**pior host**:

- **verde** — tudo abaixo de 75% do crítico do sensor
- **amarelo** — algum host entre 75% e 90%
- **vermelho** — algum host acima de 90%
- **cinza** — algum host offline

Um ponto vermelho no canto acende sempre que há qualquer alerta ou host
offline; com N hosts, um número só não conta a história toda.

Menu: um submenu por host (resumo, atualizar, copiar JSON), mais overlay
liga/desliga, modo compacto (uma linha por host), cliques atravessam,
atualizar todos, copiar JSON da frota, sair. Duplo clique no overlay alterna
compacto/detalhado; botão direito fecha.

Mudanças de estado geram notificação do Windows (`"notificar": false` desliga).

## O que dispara alerta

Definido num lugar só, em `tools/sysmon_nucleo.py:avaliar()` — o tray e o
dashboard não podem divergir sobre o que é problema.

| Condição | Aviso | Crítico |
|---|---|---|
| Temperatura da CPU | 75% do `crit` do sensor | 90% do `crit` |
| Disco (bytes ou inodes) | 80% / 90% | 90% / 97% |
| Thin pool LVM (data e metadata) | 80% | 90% |
| RAM | 90% | 97% |
| Pressão PSI (`some_avg60`) | 40% | 70% |
| RAID mdadm degradado | — | sempre |
| Coleta parada no agente | > 4× o intervalo | — |
| Host inalcançável | — | offline |

Os limiares de temperatura saem do `crit` que o **próprio sensor** reporta, e
não de um número fixo: assim o mesmo config serve para hosts com hardware
diferente.

## Endpoints

```
GET /metrics   -> telemetria completa (exige token)
GET /health    -> saúde do coletor    (sem auth, para healthcheck)
```

Autenticação por `Authorization: Bearer <token>` ou `?token=<token>`.

```bash
curl -H "Authorization: Bearer $TOKEN" http://192.168.0.10:9109/metrics | jq
```

Campos: `cpu_temp`, `cpu_crit`, `cpu_percent`, `cpu_modelo`, `temps[]`,
`fans{}`, `load[]`, `mem{}`, `pressure{}`, `discos[]`, `diskio[]`, `net[]`,
`raid[]`, `thinpools[]`, `guests{}`, `so{}`, `uptime_s`, `extras{}`,
`idade_s`, `intervalo_s`, `coletor_falhas`.

`idade_s` é a idade do dado servido. Se crescer muito além de `intervalo_s`, o
agente está vivo mas parou de coletar — o `/health` devolve 503 nesse caso, e
o watchdog do systemd reinicia o serviço sozinho.

### Estendendo sem tocar no agente

Qualquer coisa que precise de root ou de executar um binário (`lvs`, `zpool`,
`smartctl`, `systemctl`) roda numa unit isolada e deposita JSON em
`/run/sysmon/<nome>.json`. O agente lê o diretório e publica o conteúdo em
`extras.<nome>`, carimbado com `_idade_s`, sem interpretar nada. O processo
exposto à rede continua sem privilégio e sem `exec`.

O `sysmon-thinpool.timer` é o exemplo que já vem pronto.

## Desenvolvimento

```bash
make teste     # testes do agente (Go) + dos clientes (Python)
make build     # compila para esta arquitetura
make dist      # amd64 e arm64
```

O agente tem testes com um `/sys` e `/proc` falsos em diretório temporário,
o que permite exercitar hardware que a máquina de desenvolvimento não tem
(AMD, RAID degradado, kernel sem PSI).

## Segurança

Leia [docs/seguranca.md](docs/seguranca.md) antes de expor qualquer porta.
Resumo: read-only, sem `subprocess`, sem root, com hardening de systemd, e
**não deve ser exposto à internet** — o HTTP é puro e o token viaja em texto
claro. Para acesso remoto use WireGuard ou Tailscale e faça bind na interface
do túnel.

## Vindo da v1

- O agente Python virou binário Go. `install.sh` agora precisa do binário ao
  lado (`make dist` na sua máquina), ou compila sozinho se houver Go no host.
- O `config.json` antigo, com `url` e `token` na raiz, **continua funcionando**
  como host único.
- O JSON de `/metrics` manteve todos os campos da v1 e ganhou novos.
- `--mounts` deixou de ter `/ /var/lib/vz` fixo: os pontos de montagem são
  descobertos do `/proc/mounts`. Passe `--mounts` para fixar manualmente.

## Solução de problemas

Consulte [docs/troubleshooting.md](docs/troubleshooting.md).

## Licença

MIT.
