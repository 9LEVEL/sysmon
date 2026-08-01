<img src="docs/icone/sysmon-128.png" width="72" align="left" alt="">

# sysmon

**Um painel para todas as suas máquinas Linux — sem servidor, sem banco, sem
nuvem.** Uma janela na sua área de trabalho que fica vermelha quando algum host
esquenta, enche ou cai.

<br clear="left">

![O sysmon na sua máquina consulta vários hosts Linux com agente Go (GET /metrics + Bearer token); cada host responde com suas métricas, exibidas numa janela e num ícone de bandeja](docs/fluxo.svg)

---

## Comece em 3 passos

Assumo que você está no Windows e tem pelo menos um host Linux para monitorar.
Leva uns cinco minutos.

**1. No host Linux**, instale o agente. Ele é um binário estático — sem runtime,
sem `pip`, sem root:

```bash
curl -LO https://github.com/9LEVEL/sysmon/releases/latest/download/sysmon-agent-linux-amd64.tar.gz
tar xzf sysmon-agent-linux-amd64.tar.gz && cd sysmon-agent-*
sudo ./install.sh 192.168.0.10        # o IP da LAN deste host
```

No fim ele imprime a **url** e o **token**. Copie os dois.

**2. No Windows**, baixe [`sysmon-windows-<versão>.zip`][releases], extraia numa
pasta e dê duplo clique em `sysmon.exe`.

**3. A janela abre na tela de configuração.** Cole a url e o token, clique em
**TESTAR**, depois em **SALVAR**.

Pronto — é isso. Para os próximos hosts, repita o passo 1 e clique em `+ HOST`.

> Tem vários? O [`deploy.sh`](docs/instalacao.md) instala em N máquinas por SSH
> de uma vez e já escreve o arquivo de configuração pronto.

[releases]: https://github.com/9LEVEL/sysmon/releases/latest

---

## O que você vê

```
 sysmon  2 hosts · 1 alerta                    ▲ ⭳ 🔔 ⚠ ☰ ⌂ │ ─ ✕

 ⌄ PVE      9.1G de 14.9G · Core i7-9700K      52C · cpu 44% · ram 61%
 DESEMPENHO
   cpu        8 nucleos        ▅▆▁▅▆▁▅▆ ███······              44%
   memoria    9.1G / 14.9G     ▁▁▂▃▃▄▄▅ █████·····             61%
   carga      5m 0.92 · 15m 1.10                            0.84
 DISCOS
   nvme0n1    11% usado · 477G · Samsung SSD 970 EVO          43C

 ⌄ NAS      3.2G de 8.0G · Celeron J4125      41C · cpu 9% · ram 40%

 ! pve: disco /backup em 96%
```

- **Um processo, na sua máquina.** Nada roda "na nuvem": a janela é quem busca
  os dados, direto dos hosts, na sua LAN ou por um túnel (WireGuard/Tailscale).
- **Fica na bandeja** e muda de cor pelo pior host. Avisa quando a severidade
  sobe — e só quando sobe.
- **Se atualiza sozinha.** O botão ⭳ baixa a versão nova, confere o SHA256 e
  reinicia. Sem ir ao GitHub, sem descompactar nada.
- **Também roda no terminal**: `sysmon term --once` sai com código 0, 1 ou 2
  conforme o pior estado da frota — serve direto num cron.

→ [A janela em detalhe, e o que cada cor significa](docs/interface.md)

## Para quem é, e para quem não é

Fiz para **4 a 10 hosts**. Acima disso ele perde o propósito: o valor está em
ver a frota inteira junta, de relance, sem rolar — e trinta hosts não cabem numa
tela nem numa cabeça.

**Serve bem para:**

| Caso | Como fica |
|---|---|
| **Homelab** — um Proxmox, um NAS, um Pi | janela estreita encostada na lateral, sempre à vista; os hosts ociosos recolhidos |
| **Sysadmin solo** — meia dúzia de VPS | bandeja no logon, notificação só quando a severidade sobe |
| **Antes/depois de uma manutenção** | `F5` força a coleta em vez de esperar o ciclo |
| **Vigiar um disco suspeito** | gráfico do topo fixo naquele host, medida em temperatura |
| **Cron / script** | `sysmon term --once` e o código de saída |
| **Conferir a máquina local** | `sysmon local` lê `/proc` direto, sem agente nem token |

**Não serve para:** histórico longo com gráficos, alerta por e-mail ou webhook,
múltiplos usuários, centenas de hosts. Para isso existem Zabbix, Prometheus e
Grafana — e eles fazem melhor. O sysmon ocupa o espaço em que montar aquilo
custa mais do que o problema que resolve.

## A saúde dos discos é o diferencial

Um SSD não avisa que vai morrer com um campo dizendo isso. Ele expõe umas trinta
contagens, e a maioria das ferramentas ou repassa a tabela crua — que ninguém lê
— ou reduz tudo a um `PASSED` que só vira `FAILED` quando já não há o que fazer.

Aqui as contagens passam por regras, e o agente guarda histórico. A diferença
prática:

```
200 setores realocados, parados há um ano   →  silêncio (disco velho que funciona)
0 → 12 setores em uma semana                →  CRÍTICO (disco morrendo)
3 erros de CRC no barramento                →  "troque o CABO", não o disco
39 de 90 desligamentos inesperados          →  "olhe a ENERGIA", não o disco
```

E ele **nunca diz "disco saudável"**: entre 23% e 36% dos discos que falharam
não tinham nenhum indicador SMART. A frase honesta é *"sem indicadores de
falha"*, e é essa que aparece.

→ [Como funciona, e como ajustar](docs/smart.md)

## Alertas que você pode aceitar

Nem todo alerta tem conserto. *"89 de 206 desligamentos foram inesperados"* é um
fato do hardware: verdadeiro, útil da primeira vez, e depois disso apenas
repetido a cada 3 segundos. Alerta que não pode ser resolvido nem aceito acaba
ignorado — e, a partir daí, **todos** são.

O sino aceita o alerta e guarda o **valor** que o disparou. Some do rodapé, a
cor volta ao normal, e ele reaparece sozinho quando o valor muda: 89 aceito
volta a avisar em 90.

→ [Limiares, e o que dá para aceitar](docs/alertas.md)

## Documentação

| Assunto | Onde |
|---|---|
| Instalar em vários hosts, compilar do código | [instalacao.md](docs/instalacao.md) |
| A janela, o terminal, a bandeja, **as cores** | [interface.md](docs/interface.md) |
| O que dispara alerta, limiares, aceitar | [alertas.md](docs/alertas.md) |
| Saúde de disco (SMART) | [smart.md](docs/smart.md) |
| `config.json`, variáveis de ambiente, endpoints | [configuracao.md](docs/configuracao.md) |
| Quando algo não bate | [troubleshooting.md](docs/troubleshooting.md) |
| Segurança | [seguranca.md](docs/seguranca.md) |
| Estrutura do código, como compilar | [desenvolvimento.md](docs/desenvolvimento.md) |

## Contribuir

Este projeto nasceu de uma coceira minha e cresceu com o uso. Se ele te serve,
há várias formas de ajudar — e nenhuma delas exige escrever Go.

**Abra uma issue.** É o que mais ajuda, em especial sobre:

- **Hardware que eu não tenho.** As regras de SMART casam atributos por *nome*,
  e cada fabricante escolhe os seus. Se um disco seu aparece com "coleta
  falhou", com número estranho, ou com um atributo que o sysmon ignora, me mande
  a saída de `smartctl -j -H -A -i /dev/sdX`. Já corrigi dois casos assim: um WD
  que chamava o contador de `Unexpected_Power_Loss`, e outro em que o desgaste
  vinha num id diferente do esperado.
- **Um alerta que não fez sentido.** Falso positivo é o defeito mais caro desta
  ferramenta: um alerta que você aprende a ignorar contamina todos os outros.
- **Uma tela que ficou estranha** no seu tamanho de janela ou no seu tema.

**Mande um PR.** O código é comentado explicando o *porquê* de cada decisão, e
os testes cobrem comportamento, não implementação. Rode `make teste` antes. Se
for mexer na interface, vale ler [desenvolvimento.md](docs/desenvolvimento.md)
primeiro.

**Fale dele.** Um comentário num fórum de homelab, um print no Reddit, uma
estrela aqui — é assim que gente com o mesmo problema descobre que existe
solução.

## Apoiar

Se o sysmon te economizou uma tarde — ou salvou um disco antes da hora —, você
pode me pagar um café. Isso não muda nada no projeto: ele é e continua MIT, sem
versão paga e sem recurso trancado.

☕ **[9level.com.br](https://9level.com.br)**

O que muda é o ânimo de continuar mexendo nele num sábado à noite.

## Licença

MIT. Faça o que quiser, inclusive vender — só não me responsabilize.
