# Configuracao e endpoints

O `config.json`, as variaveis de ambiente e o que o agente serve.

## O `config.json`

**Na maior parte dos casos você não precisa editar nada.** A janela abre na tela
de configuração quando não há arquivo, e o botão `⌂` grava as mudanças. O que
segue é para quem prefere o editor, ou precisa de um dos casos especiais.

O arquivo fica ao lado do executável. Formato mínimo:

```json
{
  "hosts": [
    {"nome": "pve", "url": "http://192.168.0.10:9109/metrics", "token": "..."}
  ],
  "intervalo": 3
}
```

As demais chaves — `alertas`, `reconhecidos`, `ignorar_mounts` — a interface
escreve sozinha quando você mexe nos limiares ou aceita um alerta.

### Manter o token fora do arquivo

Este é o caso em que vale usar variável de ambiente: você quer versionar ou
compartilhar o `config.json` sem o segredo dentro. Omita o campo `token` do
host e defina `SYSMON_TOKEN_<NOME>`.

**O arquivo manda.** Nada do ambiente sobrescreve um valor presente no
`config.json` — o ambiente só preenche o que o arquivo não definiu. Isso é
deliberado: variável de ambiente é invisível no dia a dia, e um `SYSMON_URL`
esquecido de um teste antigo sequestraria a configuração inteira sem deixar
pista de por que o cliente está olhando para o host errado.

| Variável | Quando é usada |
|---|---|
| `SYSMON_CONFIG` | sempre — define **qual** arquivo carregar |
| `SYSMON_TOKEN_<NOME>` | só se aquele host não tiver `token` no arquivo |
| `SYSMON_URL` + `SYSMON_TOKEN` | só se o arquivo não definir host nenhum |
| `SYSMON_NOME` | nome do host acima (padrão: derivado da URL) |

No `<NOME>`, tudo que não for letra ou dígito vira `_` e o resto vira maiúscula:
o host `pve-01.lan` responde a `SYSMON_TOKEN_PVE_01_LAN`. Nome de variável não
aceita hífen nem ponto em nenhum dos dois sistemas.

### Rodar sem arquivo nenhum

Quando não há `config.json`, `SYSMON_URL` + `SYSMON_TOKEN` bastam para um host
avulso — útil para checar um agente sem configurar nada:

```bash
SYSMON_URL=http://192.168.0.10:9109/metrics SYSMON_TOKEN=... \
  sysmon term --once
```

### No Windows, com `setx`

Só é preciso se você optou por manter o token fora do arquivo, acima.

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
