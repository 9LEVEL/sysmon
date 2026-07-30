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
        │  janela nativa   │              │    bandeja +     │
        │  (ou terminal)   │              │   notificações   │
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
| `sysmon.py` | sua máquina | ponto de entrada único dos clientes |
| `tools/sysmon_nucleo.py` | clientes | config, polling e regras de alerta (compartilhado) |
| `tools/sysmon_web.py` + `tools/web/` | sua máquina | dashboard no browser (gauges, discos, SMART) |
| `tools/sysmon_dash.py` | sua máquina | dashboard de N hosts no terminal |
| `tools/sysmon_tray.py` | Windows | ícone de bandeja + overlay multi-host |
| `tools/sysmon_local.py` | um host | leitor local de sensores, sem rede nem token |
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
gh release download v2.3.0 -p 'sysmon-agent-2.3.0-linux-amd64.tar.gz'
scp sysmon-agent-2.3.0-linux-amd64.tar.gz root@192.168.0.10:/tmp/

# no host
cd /tmp && tar xzf sysmon-agent-2.3.0-linux-amd64.tar.gz
cd sysmon-agent-2.3.0-linux-amd64
sudo ./install.sh 192.168.0.10          # IP da LAN ou do túnel
```

Para puxar direto do host, é preciso um token com leitura neste repositório.
A URL de browser (`/releases/download/...`) devolve 404 mesmo com token; use o
endpoint de API do asset:

```bash
# no host, com GH_TOKEN exportado
ASSET=$(curl -s -H "Authorization: Bearer $GH_TOKEN" \
  https://api.github.com/repos/9LEVEL/sysmon/releases/tags/v2.3.0 \
  | grep -B2 '"name": "sysmon-agent-2.3.0-linux-amd64.tar.gz"' \
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

## Adicionar mais hosts depois

São dois passos: instalar o agente no host novo e acrescentá-lo ao config dos
clientes. Nada precisa ser reiniciado nos hosts que já funcionam.

**Com `deploy.sh`** — acrescente a linha ao `hosts.conf` e rode de novo. Ele é
idempotente: nos hosts já instalados o token existente é mantido, e o
`hosts.json` sai regenerado com todo mundo.

```bash
cd linux-agent
echo "novo   root@192.168.0.30   192.168.0.30" >> hosts.conf
./deploy.sh hosts.conf
```

**Na mão** — instale no host novo, anote a URL e o token que o `install.sh`
imprime, e acrescente ao `config.json` dos clientes:

```json
{
  "hosts": [
    {"nome": "pve", "url": "http://192.168.0.10:9109/metrics", "token": "..."},
    {"nome": "nas", "url": "http://192.168.0.20:9109/metrics", "token": "..."},
    {"nome": "novo", "url": "http://192.168.0.30:9109/metrics", "token": "..."}
  ]
}
```

Regras do `nome`: precisa ser único (é a chave em toda a interface) e é o que
aparece na tabela, no menu do tray e nos alertas. Se você omitir, ele é
derivado do host da URL.

Depois de editar, reinicie o tray (menu → **Sair**, e abra de novo) — o
`config.json` é lido uma vez, no arranque. O terminal basta rodar de novo.

Confira antes de mexer no tray:

```bash
python3 sysmon.py term --config config.json --once
```

## Os clientes

Tudo roda a partir de **um arquivo**. Baixe o `sysmon.pyz` do release, ponha o
`config.json` ao lado, e:

```bash
python sysmon.pyz
```

Isso abre uma **janela do sistema** com o dashboard dentro — sem barra de
endereço, sem aba de browser — e o ícone de bandeja junto, no mesmo processo.
Um autostart, um processo.

Atualizar é substituir o `sysmon.pyz`. O `config.json` fica.

| Comando | O que faz |
|---|---|
| `python sysmon.pyz` | janela nativa + bandeja (padrão) |
| `python sysmon.pyz --oculto` | sobe minimizado na bandeja (autostart) |
| `python sysmon.pyz --browser` | força o browser em vez da janela |
| `python sysmon.pyz web` | só serve a página, não abre nada |
| `python sysmon.pyz term` | tabela no terminal, atualiza sozinha |
| `python sysmon.pyz term --once` | imprime uma vez e sai (script/cron) |
| `python sysmon.pyz term --host pve` | detalhe completo de um host |
| `python sysmon.pyz tray` | só o ícone de bandeja |
| `python sysmon.pyz local` | sensores **desta** máquina, sem rede |

Rodando do repositório em vez do `.pyz`, troque `sysmon.pyz` por `sysmon.py` —
os argumentos são os mesmos.

### A janela

Gauges de temperatura, CPU e RAM por host, cada disco físico com modelo,
temperatura, taxa de I/O e vida consumida do SMART, filesystems, thin pool,
rede e RAID.

Sem framework e sem CDN: HTML, CSS e SVG puros. A janela usa o motor web que o
sistema já tem — **WebView2** no Windows (vem com o Edge), **WebKitGTK** no
Linux. Nada de Chromium embutido.

**Sempre no topo**: o botão **Fixar** no cabeçalho mantém a janela sobre todas
as outras, e o estado sobrevive ao fechar. Isso é propriedade da janela do
sistema — nenhuma página web consegue fazer isso sozinha, e é a razão de
existir a janela nativa em vez de uma aba.

A posição e o tamanho ficam em `%APPDATA%\sysmon\janela.json` (ou
`~/.config/sysmon/janela.json`) — **fora** do seu `config.json`, que o programa
nunca reescreve.

Sem `pywebview` instalado, cai no browser e diz o motivo:

```
pip install pywebview
```

**Os tokens não chegam ao browser.** O polling acontece no processo local e a
página recebe apenas telemetria. Por isso ele escuta só em `127.0.0.1`: a
página não tem autenticação, então expor na rede entregaria a telemetria da
frota inteira. Para mudar mesmo assim, `--host 0.0.0.0` (com aviso no terminal).

### Dashboard no terminal

```
sysmon  3 host(s)  1 alerta(s)                                    14:32:05

HOST       CPU    TEMP   RAM    SWAP   DISCO             LOAD              REDE               PSI-IO  UP
pve        12%    47C    38%    2%     / 62%             0.40 0.30 0.20    v1.2M/s ^340K/s    0%      12d3h
nas        4%     39C    22%    --     /tank 91%         0.10 0.00 0.00    v12K/s ^8K/s       3%      45d0h
vps        offline: sem conexao (timed out)

! nas: disco /tank em 91%
```

A tabela descarta colunas conforme o terminal estreita. `--once` e `--host`
saem com código 1 quando há host crítico ou offline, então dá para usar em
cron ou health check.

### Camadas opcionais

Nenhuma delas impede o programa de subir; o que muda é a moldura em volta:

| Instalado | O que você ganha |
|---|---|
| nada | dashboard abre no browser |
| `pywebview` | janela do sistema, sem barra de endereço, com **Fixar** |
| `pystray` + `pillow` | ícone de bandeja junto, com notificação |

```
pip install pywebview pystray pillow
```

### Bandeja

Opcional. Sem ela o dashboard sobe normalmente e o motivo aparece no terminal.

O ícone mostra a temperatura do **host mais quente**, colorido pelo **pior
host**: verde abaixo de 75% do crítico do sensor, amarelo entre 75% e 90%,
vermelho acima, cinza se algum host está offline. Um ponto vermelho no canto
acende para qualquer alerta.

Ao lado da janela nativa o menu é enxuto: **Mostrar janela**, **Sempre no
topo**, atualizar, sair. O overlay do tkinter seria redundante com a janela —
para tê-lo, use `sysmon.pyz tray`, que traz o modo antigo com submenu por host,
modo compacto e cliques atravessando.

Autostart:

```powershell
powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
```

Registra uma tarefa que sobe tudo 30s após o login, sem janela de console e
**minimizado na bandeja** — no login a janela não pula na frente do que você
está fazendo; clique no ícone para abrir.

## Configuração dos clientes

Os dois clientes leem o mesmo arquivo, e **o arquivo manda**. Nada do ambiente
sobrescreve um valor presente no `config.json` — o ambiente só preenche o que o
arquivo não definiu.

Isso é deliberado. Variável de ambiente é invisível no dia a dia: um
`SYSMON_URL` esquecido de um teste antigo sequestraria a configuração inteira
sem deixar pista de por que o cliente está olhando para o host errado.

| Variável | Quando é usada |
|---|---|
| `SYSMON_CONFIG` | sempre — define **qual** arquivo carregar |
| `SYSMON_TOKEN_<NOME>` | só se aquele host não tiver `token` no arquivo |
| `SYSMON_URL` + `SYSMON_TOKEN` | só se o arquivo não definir host nenhum |
| `SYSMON_NOME` | nome do host acima (padrão: derivado da URL) |

No `<NOME>`, tudo que não for letra ou dígito vira `_` e o resto vira
maiúscula: o host `pve-01.lan` responde a `SYSMON_TOKEN_PVE_01_LAN`. Nome de
variável não aceita hífen nem ponto em nenhum dos dois sistemas.

O uso prático do `SYSMON_TOKEN_<NOME>` é manter token fora do JSON: omita o
campo `"token"` no arquivo e ponha o segredo no ambiente.

### Rodar sem arquivo nenhum

Quando não há `config.json`, `SYSMON_URL` + `SYSMON_TOKEN` bastam para um host
avulso — útil para checar um agente sem configurar nada:

```bash
SYSMON_URL=http://192.168.0.10:9109/metrics SYSMON_TOKEN=... \
  python3 sysmon.py term --once
```

### No Windows

`setx` grava permanentemente, mas **só vale para processos abertos depois** —
o PowerShell onde você rodou continua sem a variável:

```powershell
setx SYSMON_TOKEN_PVE "cole-o-token-aqui"
$env:SYSMON_TOKEN_PVE = "cole-o-token-aqui"      # sessão atual também

[Environment]::GetEnvironmentVariable("SYSMON_TOKEN_PVE", "User")   # conferir
```

`setx` sozinho grava em `HKCU\Environment` (só o seu usuário); com `/M` vai
para o sistema todo e exige prompt como administrador. Valores acima de 1024
caracteres são truncados.

A tarefa agendada do autostart herda o ambiente do usuário no logon, então o
`setx` vale também para o tray iniciado automaticamente. Para apagar:

```powershell
Remove-ItemProperty -Path HKCU:\Environment -Name SYSMON_TOKEN_PVE -ErrorAction SilentlyContinue
```

Para trocar de host ou de token no dia a dia, edite o `config.json` — é o
caminho previsível, e o que a interface toda reflete.

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
| Temperatura de disco | 60 °C | 70 °C |
| Vida consumida do SSD (SMART) | 80% | 90% |
| Setores realocados | ≥ 1 | — |
| SMART reprovado | — | sempre |
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
`fans{}`, `load[]`, `mem{}`, `pressure{}`, `discos[]`, `blocos[]`, `net[]`,
`raid[]`, `thinpools[]`, `guests{}`, `so{}`, `uptime_s`, `extras{}`,
`idade_s`, `intervalo_s`, `coletor_falhas`.

`discos[]` são os **filesystems montados** (quanto está cheio). `blocos[]` são
os **discos físicos**: modelo, capacidade, tipo (`nvme`/`ssd`/`hdd`),
temperatura, taxa de leitura/escrita, ocupação e, quando o `smartctl` está
instalado, `smart` com vida consumida, horas ligado e setores realocados.

Temperatura por disco vem do sysfs: NVMe funciona direto, SATA exige o módulo
`drivetemp` — o instalador carrega e persiste automaticamente quando o kernel
suporta.

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
