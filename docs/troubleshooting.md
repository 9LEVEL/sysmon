# Solução de problemas

## Agente Linux

### "Conexão recusada" / "a máquina de destino as recusou ativamente"

É o erro mais comum na primeira instalação. Primeiro entenda o que ele diz:

- **recusada** — o pacote chegou na máquina e ninguém estava escutando naquele
  IP e porta. O host está no ar, o firewall não bloqueou.
- **timeout** — aí sim é firewall com `DROP`, ou host inacessível.

Como a mensagem é *recusada*, o problema está no agente ou no endereço, não na
rede. Descubra em qual IP ele está de fato escutando:

```bash
ss -lntp | grep 9109
systemctl status sysmon-agent
```

As três causas, em ordem de frequência:

**1. Você testou em `localhost`.** O agente escuta **somente** no IP que você
passou ao `install.sh` — nunca em `0.0.0.0`, e isso é proposital. Se você
instalou com `192.168.0.10`, então:

```bash
curl -s http://192.168.0.10:9109/health   # funciona
curl -s http://localhost:9109/health      # recusada, e é o esperado
```

**2. O cliente aponta para um IP diferente do de bind.** Acontece em host com
mais de um endereço — instalou com o IP do Tailscale e o `config.json` do tray
usa o da LAN, ou vice-versa. Compare:

```bash
grep ExecStart /etc/systemd/system/sysmon-agent.service   # IP de bind
```

Para trocar o IP de bind, edite essa linha e recarregue:

```bash
sudo systemctl daemon-reload && sudo systemctl restart sysmon-agent
```

**3. O serviço não está rodando.** Veja a seção seguinte.

### Serviço não sobe

```bash
journalctl -u sysmon-agent -n 50 --no-pager
```

Causas mais comuns, em ordem:

- **`bind: address already in use`** — já há um agente rodando, ou outra coisa
  usa a 9109. Confira com `ss -lntp | grep 9109`.
- **`nao consegui escutar em X`** — o `--host` da unit aponta para um IP que
  não existe nesta máquina. Confira com `ip -4 -brief addr show`.
- **`defina SYSMON_TOKEN`** — o `/etc/sysmon/token.env` sumiu ou está vazio.

### `Job for sysmon-agent.service failed ... start operation timed out`

A unit é `Type=notify`: o systemd espera o agente avisar que fez a primeira
coleta. Se o binário for de outra arquitetura ou não executar, o aviso nunca
chega. Teste direto:

```bash
/opt/sysmon/sysmon-agent --version
```

### Serviço reiniciando sozinho a cada minuto

É o watchdog agindo: o agente só manda o pulso depois de uma coleta
bem-sucedida, e o systemd reinicia se ficar 60s sem pulso. Isso é o
comportamento desejado quando a coleta trava — mas veja o que está travando:

```bash
journalctl -u sysmon-agent | grep -i 'panico\|watchdog'
```

### Nenhuma temperatura (`cpu_temp: null`, `temps: []`)

Os módulos do kernel não foram carregados:

```bash
apt install lm-sensors
sensors-detect        # responda YES em tudo
sensors
```

Se ainda assim nada: em **LXC** o `/sys/class/hwmon` é vazio, containers não
enxergam sensores. Rode o agente no **host**, não dentro de container. Em VM,
o normal é não haver sensor mesmo — o agente segue funcionando, só sem o
número da temperatura.

### Nenhuma ventoinha (`fans: {}`)

Comum em placas OEM (Dell, HP, Lenovo) que não expõem o Super I/O:

```bash
modprobe nct6775            # Super I/O padrão
modprobe dell-smm-hwmon     # Dell, via SMM
sensors
```

Se nenhum pegar, é limitação de firmware. Não há solução via software.

### `guests: null`

O agente não conseguiu ler `/etc/pve/.vmlist`. Confirme que a unit tem
`SupplementaryGroups=www-data`. Em host que não é Proxmox, `null` é o
esperado — o `install.sh` remove essa linha propositalmente.

### `thinpools: []`

Ou a máquina não usa LVM thin (ZFS, ext4 puro), ou o timer não está ativo:

```bash
systemctl status sysmon-thinpool.timer
systemctl start sysmon-thinpool.service
cat /run/sysmon/thinpool.json
```

### `idade_s` crescendo, `/health` devolvendo 503

O agente está de pé, mas a goroutine coletora parou de produzir amostras. O
`coletor_falhas` no `/metrics` conta os pânicos. Veja o journal; o watchdog
deve reiniciar em até 60s por conta própria.

Se `extras.<algo>._idade_s` estiver alto mas `idade_s` estiver normal, o
problema é no **timer auxiliar**, não no agente:

```bash
systemctl list-timers | grep sysmon
```

### Falta um disco no `discos[]`

Os pontos de montagem vêm do `/proc/mounts`, filtrados por tipo de filesystem
(`ext4`, `xfs`, `btrfs`, `zfs`, `nfs`, `cifs`...). Filesystems fora dessa
lista são ignorados de propósito — `tmpfs` e `overlay` só gerariam ruído.
Além disso, montagens que apontam para o mesmo device (bind mounts) aparecem
uma vez só.

Para fixar manualmente, edite a unit e passe `--mounts`:

```
ExecStart=/opt/sysmon/sysmon-agent --host ... --mounts /,/var/lib/vz,/tank
```

### `pressure: null`

O kernel não tem `CONFIG_PSI` habilitado, ou é anterior ao 4.20. Confira com
`cat /proc/pressure/cpu`. Em Debian 12+ e Ubuntu 22.04+ vem ligado.

### Interfaces de rede demais / de menos

Prefixos virtuais (`veth`, `tap`, `fwbr`, `docker`, `br-`...) são descartados;
sem isso um host com 20 VMs devolveria 60 interfaces. Para mudar a lista:

```
ExecStart=/opt/sysmon/sysmon-agent --host ... --net-ignorar lo,veth,docker
```

### Swap em uso com RAM sobrando

Não é bug do agente, é o `vm.swappiness=60` padrão do Debian. Em host de
virtualização isso adiciona latência às VMs sem necessidade:

```bash
echo "vm.swappiness = 10" > /etc/sysctl.d/99-swappiness.conf
sysctl -p /etc/sysctl.d/99-swappiness.conf
swapoff -a && swapon -a      # só se houver RAM livre suficiente
```

## Deploy em vários hosts

### `deploy.sh` diz "sem acesso SSH"

Ele usa `BatchMode=yes` de propósito, para não travar o laço inteiro esperando
senha. Configure chave antes:

```bash
ssh-copy-id root@192.168.0.20
ssh root@192.168.0.20 true      # tem que funcionar sem pedir nada
```

### "arquitetura nao suportada"

Só há build para `x86_64` (amd64) e `aarch64` (arm64). Para outra, compile na
mão: `GOOS=linux GOARCH=... go build .`

### O host instalou mas não aparece no `hosts.json`

O `deploy.sh` só inclui hosts em que a instalação terminou com sucesso. Os que
falharam são listados no fim, com o motivo.

## Clientes

### `config nao encontrado`

O cliente procura o config nesta ordem: `--config`, `$SYSMON_CONFIG`,
`./hosts.json`, `~/.config/sysmon/hosts.json`, `/etc/sysmon/hosts.json`.

`./config.json` resolve o caso comum: deixe o `config.json` ao lado do
`sysmon.pyz`. O autostart define o diretório de trabalho justamente para isso.

### Um host aparece como "offline: token invalido"

O token do `config.json` não bate com o `/etc/sysmon/token.env` daquele host.
Cada host tem token próprio:

```bash
ssh root@192.168.0.20 'grep SYSMON_TOKEN /etc/sysmon/token.env'
```

### Um host offline deixa os outros lentos?

Não. Cada host tem sua própria thread de polling, com recuo exponencial: após
falhas consecutivas o intervalo dobra até o teto de 60s. Um host desligado não
gera tentativa a cada 5s indefinidamente.

## Windows

### Duas instâncias subindo no logon

Acontece se o Agendador de Tarefas **e** a pasta Inicializar ficarem os dois
registrados — resquício de instalação anterior. A segunda instância encontra a
porta 9110 ocupada e, desde a 2.4.2, pede para a primeira aparecer e sai limpa;
antes disso ela morria em silêncio sob `pythonw`.

Conferir e limpar:

```powershell
Get-ScheduledTask -TaskName sysmon -ErrorAction SilentlyContinue   # tem tarefa?
dir "$([Environment]::GetFolderPath('Startup'))\sysmon.lnk"        # tem atalho?

schtasks /delete /tn sysmon /f                                     # remove a tarefa
del "$([Environment]::GetFolderPath('Startup'))\sysmon.lnk"        # remove o atalho
```

Rodar o `instalar-autostart.ps1` de novo também resolve: ele deixa só um.

### `Register-ScheduledTask : Acesso negado` (HRESULT 0x80070005)

Registrar tarefa na raiz da biblioteca do Agendador exige administrador. O
instalador tenta o Agendador primeiro e cai sozinho na **pasta Inicializar**
quando não há permissão — o aviso amarelo no meio da saída é isso, e não é
erro. Para forçar um dos dois:

```powershell
powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
```

O `-Agendador` é opcional e só funciona num PowerShell aberto como
administrador. A única coisa que se ganha com ele é reinício automático se o
processo cair.

Para registrar o autostart na mão, sem script:

```powershell
$p = (Get-Location).Path
$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut("$([Environment]::GetFolderPath('Startup'))\sysmon.lnk")
$lnk.TargetPath = "wscript.exe"; $lnk.Arguments = "`"$p\sysmon.vbs`""
$lnk.WorkingDirectory = $p; $lnk.Save()
```

### `sysmon.pyz nao encontrado nesta pasta`

Você provavelmente baixou o **código-fonte** (`sysmon-main.zip`), que não traz
o binário. Da [página de releases](https://github.com/9LEVEL/sysmon/releases),
baixe:

- **`sysmon-windows-<versão>.zip`** — é este no Windows; vem com o `sysmon.pyz`,
  o `sysmon.bat`, o `sysmon.vbs` e os scripts, todos na mesma pasta
- `sysmon-linux-<versão>.tar.gz` — cliente para Linux/macOS
- `sysmon-agent-<versão>-linux-*.tar.gz` — vai nos hosts **monitorados**, não
  na sua máquina

### `pythonw sysmon.pyz` não abre nada

`pythonw.exe` roda sem console: `stdout` e `stderr` vão para o vazio. Qualquer
erro de import ou de configuração mata o processo em silêncio.

**Sempre teste primeiro com `python sysmon.pyz`**, que mostra o traceback.

A bandeja já instala um `sys.excepthook` que grava em `%TEMP%\traymon.log`
e exibe uma MessageBox. Então:

```powershell
type $env:TEMP\traymon.log
```

### `ModuleNotFoundError`

O `pip` pode ter instalado em um Python diferente do que o `pythonw` resolve:

A bandeja é opcional: sem `pystray`/`Pillow` a janela sobe do mesmo jeito e o
motivo aparece no terminal. Para ter o ícone:

```powershell
python -c "import pystray, PIL, tkinter; print('ok')"
python -m pip install -r requirements.txt
```

Erro no `tkinter` significa Python da Microsoft Store, que vem sem Tk.
Instale o oficial do python.org.

### Ícone cinza / hosts "Offline"

Nesta ordem:

```powershell
Test-NetConnection 192.168.0.10 -Port 9109
curl.exe http://192.168.0.10:9109/health
curl.exe -H "Authorization: Bearer SEU_TOKEN" http://192.168.0.10:9109/metrics
```

- falha na porta → firewall do host ou serviço parado
- `/health` responde mas `/metrics` dá `401` → token diferente
- funciona no curl mas não no tray → veja `%TEMP%\traymon.log`

### O overlay ficou grande demais com muitos hosts

Use o modo compacto (uma linha por host): menu do ícone → **Overlay compacto**,
ou duplo clique no próprio overlay. Para deixar como padrão, ponha
`"overlay_compacto": true` no `config.json`.

### `schtasks`: "A opção obrigatória 'sc' está faltando"

Não falta nada — o PowerShell está mastigando as barras invertidas. O `\"` é
escape do `cmd.exe`, não do PowerShell.

```powershell
# caminho sem espaços: dispense as aspas internas
schtasks /create /tn traymon /tr "wscript.exe C:\caminho\traymon.vbs" /sc onlogon

# caminho com espaços: use --% para parar o parsing do PowerShell
schtasks --% /create /tn traymon /tr "wscript.exe \"C:\Program Files\t\traymon.vbs\"" /sc onlogon
```

Ou simplesmente use o `instalar-autostart.ps1`, que usa os cmdlets nativos e
não tem esse problema.

### Variável de ambiente some ao reiniciar

`set` (cmd) e `$env:` (PowerShell) valem só na sessão atual. `setx` é
permanente, mas só afeta processos abertos **depois** dele.

Por isso o projeto usa `config.json`: não depende de ambiente, sobrevive a
reboot e é fácil de editar.
