# Desenvolvimento

## Estrutura

| Caminho | Onde roda | O que é |
|---|---|---|
| `cmd/sysmon-agent` | cada host | agente HTTP read-only, binário estático |
| `cmd/sysmon` | sua máquina | o cliente: um binário, sem runtime |
| `internal/metricas` | os dois | **o contrato do fio, definido uma vez só** |
| `internal/coleta` | agente | lê `/proc`, `/sys` e o SMART do smartctl |
| `internal/historico` | agente | série temporal dos contadores SMART |
| `internal/smart` | cliente | as regras de saúde de disco (função pura) |
| `internal/nucleo` | cliente | config, polling e regras de alerta |
| `internal/tela` | cliente | o que aparece — compartilhado pela janela e pelo terminal |
| `internal/janela` | cliente | **janela nativa (Gio), o padrão** |
| `internal/terminal` | cliente | tabela no terminal |
| `internal/bandeja` | Windows | ícone de bandeja em Win32 puro |
| `internal/atualizar` | cliente | troca o próprio binário pela versão nova |
| `linux-agent/install.sh` | cada host | instala, gera token e testa |
| `linux-agent/deploy.sh` | sua máquina | instala em N hosts por SSH e gera o config |
| `linux-agent/sysmon-agent.service` | cada host | unit systemd com hardening + watchdog |
| `linux-agent/sysmon-smart.{sh,service,timer}` | cada host | roda o `smartctl` numa unit isolada |
| `linux-agent/sysmon-thinpool.{service,timer}` | Proxmox | snapshot do thin pool LVM |
| `windows-tray/instalar-autostart.ps1` | Windows | registra no Agendador de Tarefas |

**Um módulo Go só**, com dois binários. Até a v5.0 eram dois módulos e o
contrato do fio existia em duas cópias, com um teste comparando as tags JSON
para que não divergissem em silêncio. Agora os dois lados leem a mesma
definição — que é a única garantia que não depende de alguém lembrar de rodar
nada.

## Desenvolvimento

```bash
make teste     # vet, gofmt e testes de tudo (o agente também com -race)
make versao    # confere se todo lugar declara a mesma versão
make agente    # compila o agente para esta arquitetura
make cliente   # compila o cliente para Windows e Linux
make pacote    # gera os pacotes de distribuição em dist/
```

A coleta tem testes com um `/sys` e `/proc` falsos em diretório temporário, o
que permite exercitar hardware que a máquina de desenvolvimento não tem (AMD,
RAID degradado, kernel sem PSI, disco com setor pendente).

As regras de `internal/smart` são **função pura** sobre uma leitura já
normalizada: não dependem de rede, arquivo nem relógio, e por isso a
especificação inteira é testável sem disco nenhum.

## Vindo da v1

- O agente Python virou binário Go. `install.sh` agora precisa do binário ao
  lado (`make agente` na sua máquina), ou compila sozinho se houver Go no host.
- O `config.json` antigo, com `url` e `token` na raiz, **continua funcionando**
  como host único.
- O JSON de `/metrics` manteve todos os campos da v1 e ganhou novos.
- `--mounts` deixou de ter `/ /var/lib/vz` fixo: os pontos de montagem são
  descobertos do `/proc/mounts`. Passe `--mounts` para fixar manualmente.
