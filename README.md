# sysmon

**Um painel para todas as suas máquinas Linux — sem servidor, sem banco, sem
nuvem.** Uma janela na sua área de trabalho que fica vermelha quando algum host
esquenta, enche ou cai.

![O sysmon na sua máquina consulta vários hosts Linux com agente Go (GET /metrics + Bearer token); cada host responde com suas métricas, exibidas numa janela e num ícone de bandeja](docs/fluxo.svg)

## A dor que ele resolve

Você tem um Proxmox, um servidorzinho de casa, talvez uma VPS. Para saber se
algum está com disco cheio, CPU fervendo ou simplesmente offline, a escolha
costuma ser abrir SSH em cada um — ou montar um Grafana + Prometheus, com banco
de série temporal, um _exporter_ em cada host e um servidor no ar só para isso.

O sysmon é o meio-termo que faltava:

- **No host:** um binário Go estático. Instalar é copiar um arquivo. Sem
  runtime, sem `pip`, sem `exec`, sem root — ele lê `/sys` e `/proc` e serve por
  HTTP, atrás de um token.
- **Na sua máquina:** um arquivo. Abre uma **janela nativa** (e um ícone de
  bandeja) que busca os dados de todos os agentes e mostra tudo junto. O ícone
  muda de cor pelo pior host e **notifica quando algo muda**.

Nada roda "na nuvem": a janela é o servidor. Ela puxa as métricas direto dos
hosts, na sua LAN ou por um túnel (WireGuard/Tailscale). Um autostart, um
processo.

## Como funciona

1. **Instale o agente** em cada host Linux — um `install.sh`, ou o `deploy.sh`
   fazendo N hosts por SSH de uma vez.
2. Cada agente serve **`GET /metrics`** (autenticado por token) com CPU,
   temperatura, RAM, discos, SMART, RAID, thin pool LVM e rede.
3. **Abra o cliente** na sua máquina: a **janela nativa + bandeja** (o padrão)
   ou a **tabela no terminal**. Os dois leem os mesmos hosts e **as mesmas
   regras de alerta**, definidas num lugar só.

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
| `tools/sysmon_dash.py` | sua máquina | dashboard de N hosts no terminal |
| `tools/sysmon_win.py` | sua máquina | **janela nativa (Tkinter), o padrão** |
| `tools/sysmon_tray.py` | Windows | ícone de bandeja + overlay multi-host |
| `tools/sysmon_local.py` | um host | leitor local de sensores, sem rede nem token |
| `windows-tray/instalar-autostart.ps1` | Windows | registra no Agendador de Tarefas |

## Instalação nos hosts Linux

### Pacote pronto (sem compilar nada)

Os releases trazem o agente empacotado: binário estático, units e instalador.
Não precisa de Go, Python nem nada além de `curl` no host de destino.

```bash
# no host a ser monitorado
curl -fLO https://github.com/9LEVEL/sysmon/releases/latest/download/sysmon-agent-2.4.0-linux-amd64.tar.gz
tar xzf sysmon-agent-2.4.0-linux-amd64.tar.gz
cd sysmon-agent-2.4.0-linux-amd64
sudo ./install.sh 192.168.0.10          # IP da LAN ou do túnel
```

Para ARM, troque `amd64` por `arm64`. Confira as somas com o `SHA256SUMS` do
release.

### Qual arquivo do release baixar

| Arquivo | Para que serve |
|---|---|
| `sysmon-windows-<v>.zip` | **Windows** — app completo: `.pyz`, lançadores e instaladores |
| `sysmon-linux-<v>.tar.gz` | **Linux/macOS** — cliente (janela e terminal) |
| `sysmon-agent-<v>-linux-amd64.tar.gz` | agente, em **cada host monitorado** (x86_64) |
| `sysmon-agent-<v>-linux-arm64.tar.gz` | agente, em **cada host monitorado** (ARM) |
| `sysmon.pyz` | só o programa, para substituir manualmente |
| `SHA256SUMS` | conferência |

Repare que são coisas diferentes: o **agente** vai nas máquinas Linux que você
quer monitorar; o **cliente** vai na máquina de onde você olha.

Para gerar os pacotes você mesmo: `make pacote` (roda a suíte de testes antes).
E o **`deploy.sh` faz tudo por SSH** — compila, copia e instala em N hosts, sem
passar por release nenhum.

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

Tudo roda a partir de **um arquivo**, **na sua máquina** — sem servidor nem
página web. O `sysmon.pyz` busca os dados direto dos agentes remotos e mostra
tudo numa janela nativa (o diagrama no topo mostra o fluxo). Baixe o `.pyz` do
release, ponha numa pasta, e:

**Windows:** duplo clique em `sysmon.bat` (abre com console, então qualquer
erro aparece). **Linux/macOS:** `python3 sysmon.pyz`.

Na primeira vez a janela abre na **tela de configuração** — preencha apelido,
URL e token de cada host, clique em **Testar** e salve. Não precisa editar
arquivo nenhum. Depois, o botão **⌂** no cabeçalho reabre essa tela a qualquer
momento.

É uma **janela do sistema** — sem barra de endereço, sem aba de browser — com o
ícone de bandeja junto, no mesmo processo. Um autostart, um processo.

Atualizar é substituir o `sysmon.pyz`. O `config.json` fica.

| Comando | O que faz |
|---|---|
| `python sysmon.pyz` | janela nativa + bandeja (padrão) |
| `python sysmon.pyz --oculto` | sobe minimizado na bandeja (autostart) |
| `python sysmon.pyz term` | tabela no terminal, atualiza sozinha |
| `python sysmon.pyz term --once` | imprime uma vez e sai (script/cron) |
| `python sysmon.pyz term --host pve` | detalhe completo de um host |
| `python sysmon.pyz tray` | só o ícone de bandeja (overlay multi-host) |
| `python sysmon.pyz local` | sensores **desta** máquina, sem rede |

Rodando do repositório em vez do `.pyz`, troque `sysmon.pyz` por `sysmon.py` —
os argumentos são os mesmos.

### Atualização automática

O sysmon verifica se há versão nova ao iniciar e a cada 6 horas, baixa em
segundo plano e **confere o SHA256 contra o `SHA256SUMS` do release**. O
download vai para `sysmon-novo.pyz` e **entra no próximo arranque**: quando você
fecha e abre de novo, o lançador (`sysmon.vbs`/`sysmon.bat`) promove o arquivo
antes do Python abri-lo — no Windows um processo não consegue sobrescrever com
segurança o próprio `.pyz` que tem aberto.

Se o SHA não bater, ou o download não for um zipapp válido, **nada é trocado** —
o erro fica registrado e a versão atual continua. Falha de rede também nunca
derruba o monitoramento.

Para desligar: `--sem-update`, ou `"horas_entre_updates": 0` no `config.json`.

Rodando do repositório (`sysmon.py` em vez de `sysmon.pyz`) o auto-update fica
inativo — ali quem atualiza é o `git`.

### A janela

Sem moldura, escura, monoespaçada. No corpo, **nenhum ícone**: a hierarquia e a
severidade saem de tipografia, alinhamento e cor.

```
 sysmon  1 host · 1 alerta

 ▾ MAQUINA     3G de 15G · Ubuntu 26.04 LTS    39C · cpu 5% · ram 21%
     DESEMPENHO
       cpu      8 nucleos · Core i3-12100   ▅▆▁▅▆▁▅▆ ███······    5%
       memoria  3G / 15G                    ▁▁▁▁▁▁▁▁ ██········   21%
       carga    0.83 5m · 0.54 15m                             1.33
     TEMPERATURA
       cpu      critico 100C                ▃▅▂▆▄▃▅▂             39C
```

No **cabeçalho**, à direita, uma fileira de ícones desenhados a vetor — não
dependem de fonte, então aparecem iguais em qualquer sistema: **sempre no topo**
(acende em azul quando ligado), **atualizar**, **limiares de alerta**, **escolher
o que exibir** e **hosts**; depois de um separador, **minimizar** e **fechar** (que
fica vermelho ao passar o mouse). Cada ícone diz o que faz no hover.

**Três colunas com papéis fixos.** O nome nunca é truncado; o detalhe no meio é
quem cede quando você estreita a janela; os números ficam **colados na borda
direita** e acompanham o redimensionamento.

#### Cinco degraus de cor, não três

Sair de 3% para 30% de CPU é a variação que interessa no dia a dia, e com
apenas ok/aviso/crítico os dois eram a mesma cor:

| Faixa | Cor | Leitura |
|---|---|---|
| < 20% | cinza apagado | ocioso, a linha recua |
| 20–50% | branco | trabalhando |
| 50% até o aviso | ciano | notável |
| ≥ aviso | âmbar | atenção |
| ≥ crítico | vermelho | agora |

Âmbar e vermelho continuam significando **alerta** — por isso a faixa
intermediária usa ciano, e não um amarelo mais claro que competiria com eles.

#### Sparkline

`▅▆▁▅▆▁▅▆` ao lado da barra mostra os últimos ciclos. A barra responde "quanto
agora"; o sparkline responde **"está subindo ou é o normal dele?"** — que é o
que a barra sozinha não conta.

A escala é automática **com piso de amplitude**: oscilar entre 3,0% e 3,2%
continua parecendo o que é (linha reta), mas subir de 3% para 30% preenche o
desenho. Escala fixa 0–100 achataria a variação útil; autoescala pura
transformaria ruído em drama.

**Arrasta pelo cabeçalho**, redimensiona pelo canto inferior direito. O `▲`
prende sobre as outras janelas. Botão direito no topo abre o menu — inclusive
para trazer a moldura do sistema de volta.

**Não depende de nada.** É Tkinter, que vem junto com o Python do python.org.

O `⌂` abre a configuração de hosts, com botão **Testar**.

### Limiares de alerta

O `!` abre onde cada medida vira **aviso** (âmbar) e **crítico** (vermelho):
temperatura da CPU, temperatura de disco, memória, filesystem, inodes, thin
pool, desgaste do SSD e pressão PSI. `PADRAO` volta tudo ao original.

A temperatura da CPU é configurada como **fração do crítico que o próprio
sensor reporta** — `0.75` significa aviso a 75% do limite do chip. É o que faz
o mesmo número servir para hardwares diferentes; o par fixo em °C só entra
quando o sensor não informa o crítico.

Na mesma tela ficam os **filesystems ignorados**, um por linha. O padrão já traz
`/boot` e `/boot/efi`: são partições de tamanho fixo cujo percentual não diz
nada útil — `/boot` enche de kernel antigo e a ESP vive quase cheia por
natureza. Alertar nelas ensina a ignorar alerta.

O filtro vale para a janela e o terminal, e também para o alerta.

### Escolher o que aparece

O `☰` abre a lista de tudo que a ferramenta coleta, agrupado por seção, com
uma caixa para cada campo:

```
☑ RESUMO  na linha do host        ☑ TEMPERATURA
  ☑ temperatura da cpu              ☑ cpu
  ☑ uso de cpu                      ☑ demais sensores do hardware
  ☑ uso de memoria em %           ☑ DISCOS
  ☑ memoria usada em GB             ☑ modelo, temperatura, desgaste, SMART
  ☑ sistema operacional           ☑ ARMAZENAMENTO
☑ DESEMPENHO                        ☑ filesystems montados
  ☑ uso de cpu                      ☑ thin pool LVM (Proxmox)
  ☑ memoria                       ☑ REDE
  ☑ swap                            ☑ interfaces ativas
  ☑ carga (load average)
  ☑ tempo no ar
```

A lista mostra **tudo**, mesmo o que você desmarcou — ela serve também de
inventário do que está sendo coletado. Desmarcar uma seção esconde o bloco
inteiro.

**Esconder não silencia alerta.** Se você tirar o thin pool da tela e ele
encher, o aviso continua aparecendo no rodapé. Preferência de exibição não
deve virar um jeito silencioso de perder problema — há teste garantindo isso.

A escolha fica em `%APPDATA%\sysmon\janela-tk.json`, junto de tamanho e
posição. O arquivo guarda o que está **escondido**, não o que está visível:
assim um campo novo numa versão futura aparece por padrão, em vez de nascer
oculto para quem já tinha preferência salva.

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

### É um app de bandeja

A janela usa **Tkinter, que vem com o Python** — abre sem instalar nada. Para o
**ícone de bandeja**, dois pacotes opcionais (`pystray` e `pillow`); com eles, o
sysmon se comporta como qualquer app de bandeja:

- **fechar a janela não encerra o programa**: ele fica no ícone da bandeja, e
  a janela reabre pelo ícone ou pelo atalho
- **Sair** no menu da bandeja é o que encerra de verdade
- o ícone muda de cor conforme o pior host, e notifica quando algo muda

```
python -m pip install pystray pillow
```

No Windows o `sysmon.bat` já instala esses dois sozinho na primeira execução.
Sem eles, a janela funciona igual — só não há ícone na bandeja.

### Bandeja

Opcional. Sem ela a janela sobe normalmente e o motivo aparece no terminal.

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

Ele tenta o **Agendador de Tarefas** e, se não houver permissão de
administrador, cai na **pasta Inicializar** — que não exige nada. Os dois
funcionam; o Agendador dá a mais o reinício automático se o processo cair.

Seja qual for o caminho, **o outro é removido**: se os dois ficassem ativos,
duas instâncias subiriam no logon e uma morreria disputando a porta. O script
avisa se detectar os dois.

Para forçar um deles:

```powershell
... -Agendador      # exige admin; falha em vez de cair no outro
... -Inicializar    # nem tenta o Agendador
```

No login o sysmon sobe **minimizado na bandeja**, após 20s de espera (para a
rede estabilizar). Clique no ícone da bandeja, ou no atalho da área de
trabalho, para abrir.

Abrir uma segunda vez com o sysmon já rodando **traz a janela existente para a
frente** em vez de tentar subir de novo — vale para o duplo clique no atalho e
para autostart duplicado.

Remover: `desinstalar-autostart.ps1` (limpa os dois caminhos).

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

Definido num lugar só, em `tools/sysmon_nucleo.py:avaliar()` — a janela, o
terminal e a bandeja não podem divergir sobre o que é problema.

| Condição | Aviso | Crítico |
|---|---|---|
| Temperatura da CPU | 75% do `crit` do sensor | 90% do `crit` |
| Disco (bytes ou inodes) | 80% / 90% | 90% / 97% |
| Thin pool LVM (data e metadata) | 80% | 90% |
| RAM | 90% | 97% |
| Pressão PSI (`some_avg60`) | 40% | 70% |

Todos configuráveis pelo `!` na janela, ou pela chave `alertas` do
`config.json`. `/boot` e `/boot/efi` são ignorados por padrão.
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
