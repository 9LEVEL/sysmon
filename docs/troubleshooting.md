# Solução de problemas

## Agente Linux

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

O `sysmon-dash.py` procura, nesta ordem: `--config`, `$SYSMON_CONFIG`,
`./hosts.json`, `~/.config/sysmon/hosts.json`, `/etc/sysmon/hosts.json`.

O `traymon.py` usa `$SYSMON_CONFIG` ou o `config.json` **na pasta do script** —
resolvido a partir de `sys.argv[0]`, não do diretório de trabalho, porque o
Agendador de Tarefas inicia o processo em outro lugar.

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

### `pythonw traymon.py` não abre nada

`pythonw.exe` roda sem console: `stdout` e `stderr` vão para o vazio. Qualquer
erro de import ou de configuração mata o processo em silêncio.

**Sempre teste primeiro com `python traymon.py`**, que mostra o traceback.

O `traymon.py` já instala um `sys.excepthook` que grava em `%TEMP%\traymon.log`
e exibe uma MessageBox. Então:

```powershell
type $env:TEMP\traymon.log
```

### `sysmon_nucleo.py nao encontrado`

O tray importa o núcleo compartilhado de `tools/`. Ou clone o repositório
inteiro no Windows, ou copie `tools/sysmon_nucleo.py` para dentro da pasta
`windows-tray` — o script procura nos dois lugares.

### `ModuleNotFoundError`

O `pip` pode ter instalado em um Python diferente do que o `pythonw` resolve:

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
