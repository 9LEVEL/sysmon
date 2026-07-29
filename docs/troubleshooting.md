# Solução de problemas

## Linux

### Nenhuma temperatura aparece (`temps: []`)

Os módulos do kernel não foram carregados:

```bash
apt install lm-sensors
sensors-detect        # responda YES em tudo
sensors
```

Se ainda assim nada: em **LXC** o `/sys/class/hwmon` é vazio, containers não
enxergam sensores. Rode o agente no **host**, não dentro de container.

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
`SupplementaryGroups=www-data`. Em host que não é Proxmox, `null` é o esperado.

### `thinpools: []`

Ou a máquina não usa LVM thin (ZFS, ext4 puro), ou o timer não está ativo:

```bash
systemctl status sysmon-thinpool.timer
systemctl start sysmon-thinpool.service
cat /run/sysmon/thinpool.json
```

### Serviço não sobe

```bash
journalctl -u sysmon-agent -n 50 --no-pager
```

Causa mais comum: o `--host` da unit aponta para um IP que não existe nesta
máquina. Confira com `ip -4 -brief addr show`.

### Swap em uso com RAM sobrando

Não é bug do agente, é o `vm.swappiness=60` padrão do Debian. Em host de
virtualização isso adiciona latência às VMs sem necessidade:

```bash
echo "vm.swappiness = 10" > /etc/sysctl.d/99-swappiness.conf
sysctl -p /etc/sysctl.d/99-swappiness.conf
swapoff -a && swapon -a      # só se houver RAM livre suficiente
```

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

### `ModuleNotFoundError`

O `pip` pode ter instalado em um Python diferente do que o `pythonw` resolve:

```powershell
python -c "import pystray, PIL, tkinter; print('ok')"
python -m pip install -r requirements.txt
```

Erro no `tkinter` significa Python da Microsoft Store, que vem sem Tk.
Instale o oficial do python.org.

### Ícone cinza / "Offline"

Nesta ordem:

```powershell
Test-NetConnection 10.0.0.5 -Port 9109
curl.exe -H "Authorization: Bearer SEU_TOKEN" http://10.0.0.5:9109/metrics
```

- falha na porta → firewall do PVE ou serviço parado
- `401` → token do `config.json` diferente do `/etc/sysmon/token.env`
- funciona no curl mas não no tray → veja o log em `%TEMP%\traymon.log`

### `config.json não encontrado`

O Windows inicia a tarefa agendada em outro diretório de trabalho. O script usa
`Path(sys.argv[0]).parent` justamente para resolver isso, mas confirme que o
`config.json` está **na mesma pasta do `traymon.py`** e que a tarefa tem
`-WorkingDirectory` apontando para lá.

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
