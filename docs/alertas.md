# Alertas

O que dispara, como ajustar e como aceitar. Para a saude de disco, que tem
regras proprias, veja [smart.md](smart.md).

## O que dispara alerta

Definido num lugar só, em `internal/nucleo.Avaliar()` — a janela, o terminal e
a bandeja não podem divergir sobre o que é problema.

| Condição | Aviso | Crítico |
|---|---|---|
| Temperatura da CPU | 75% do `crit` do sensor | 90% do `crit` |
| Temperatura de disco | 60 °C | 70 °C |
| Disco (bytes ou inodes) | 80% / 90% | 90% / 97% |
| Thin pool LVM (data e metadata) | 80% | 90% |
| RAM | 90% | 97% |
| Pressão PSI (`some_avg60`) | 40% | 70% |
| RAID mdadm degradado | — | sempre |
| Coleta parada no agente | > 4× o intervalo | — |
| Host inalcançável | — | offline |
| Saúde de disco (SMART) | ver a seção abaixo | |

Todos configuráveis pelo `!` na janela, ou pela chave `alertas` do
`config.json`. `/boot` e `/boot/efi` são ignorados por padrão.

Os limiares de temperatura saem do `crit` que o **próprio sensor** reporta, e
não de um número fixo: assim o mesmo config serve para hosts com hardware
diferente.

## Aceitar um alerta

Nem todo alerta tem conserto. "89 de 206 desligamentos foram inesperados" é um
fato do hardware: verdadeiro, útil da primeira vez, e depois disso apenas
repetido a cada 3 segundos para sempre. Alerta que não pode ser resolvido nem
aceito acaba ignorado — e, a partir daí, **todos** são.

O ícone de **sino** abre *alertas e notificações*: a lista do que está
alertando agora, com um **ACEITAR** em cada linha.

Aceitar esconde o alerta do rodapé e **devolve a cor ao normal**. Não é
silenciar para sempre — o que fica guardado é o **valor** que disparou:

```
aceito:  89 de 206 desligamentos inesperados
90 de 207  →  volta a alertar
```

Vale para qualquer coisa com valor estável: contadores SMART, RAID degradado,
disco cheio, thin pool. Um RAID em `[U_]` aceito volta a gritar em `[__]`.

**Exceto** CPU, RAM, temperatura e pressão PSI. Esses sobem e descem sozinhos:
congelar "CPU em 82%" seria congelar um número que já mudou no ciclo seguinte.
Para eles a resposta certa é o **limiar**, logo abaixo. A tela mostra a linha,
sem botão, dizendo isso.

O que foi aceito não fica invisível: o ícone acende, a dica dele conta quantos
são e o rodapé mostra "3 alertas aceitos". Silêncio precisa ser visível, senão
vira esquecimento.

Fica no **banco do cliente**, o `sysmon.db` ao lado do `config.json` — e não
mais no próprio `config.json`, como até a 5.7. Na primeira abertura os aceites
antigos são importados com a data original e saem do arquivo. Veja
[O banco do cliente](#o-banco-do-cliente).

Cada aceite é **por host**: a entrada se chama `pve/smart:sda:...`, e não só
`smart:sda:...`. Até a 5.7 o host ficava de fora, e dois hosts com a mesma
condição — um `sda` com desligamentos sujos em cada — disputavam a mesma
entrada: aceitar os dois fazia um deles voltar, e os dois se revezavam no
rodapé. Entradas antigas, sem host, continuam valendo; o **ACEITAR TODOS** as
converte para o formato novo.

## O banco do cliente

O `sysmon.db` (SQLite) fica na mesma pasta do `config.json` e guarda duas
coisas: **o que você decidiu** e **o que o sysmon viu acontecer**.

**Por que um banco.** O `config.json` era gravado trocando o arquivo inteiro,
sem esperar o disco confirmar. Numa queda de energia logo depois de salvar, ele
podia voltar vazio ou na versão anterior — perdendo o aceite e, junto, os
tokens dos hosts. O banco grava em modo WAL com `synchronous=FULL`: quando o
botão responde, o aceite já está no disco. (O `config.json` também passou a
esperar o disco antes de trocar o arquivo.)

**Desde quando.** Na tela de alertas, cada um ganha uma terceira linha:

```
novo · desde 15/10 03:47                         ← apareceu nas últimas 24 h
desde 02/10 14:10                                ← antigo
já estava assim na 1a leitura, em 01/10 09:00     ← o sysmon começou a olhar já assim
novo: 89/267/33 -> 90/268/34 em 15/10 03:47 · host religou 15/10 03:46 (visto antes em 15/10 03:12)
```

A última é a de uma queda de energia: o contador de desligamentos sujos subiu,
o host religou às 03:46, e a última leitura viva antes disso foi às 03:12 — a
queda foi nesse intervalo. A hora do boot sai do uptime do próprio host, então
é exata mesmo que o cliente estivesse fechado; o "visto antes" só é tão preciso
quanto o tempo que o cliente ficou aberto.

**O que fica guardado:**

| Tabela | O quê |
|---|---|
| `aceite` | os aceites vigentes |
| `aceite_historico` | todo aceite, revogação e importação, com data — nunca é apagado |
| `alerta` | cada episódio de alerta: quando abriu, quando fechou, valor inicial e atual |
| `alerta_valor` | cada mudança de valor dentro de um episódio |
| `reinicio` | cada boot detectado de cada host, e a última leitura antes dele |

**O que não fica:** as séries de CPU, RAM e temperatura. Mudam a cada leitura,
ninguém consulta o passado delas, e gravá-las seria escrever sem parar no SSD
da máquina. O banco só grava quando algo **muda**: um alerta abre, muda de
valor ou fecha, um host reinicia. Um alerta só é dado por encerrado depois de
2 minutos sumido — temperatura oscilando no limiar não vira um episódio novo a
cada oscilação. E host fora do ar não fecha os alertas dele: o disco não sarou
porque a rede caiu.

Para consultar à mão, qualquer cliente SQLite serve:

```sql
SELECT host, chave, datetime(aberto_em, 'unixepoch', 'localtime'), valor_atual
FROM alerta WHERE fechado_em IS NULL;
```

Se o banco não puder ser aberto, nada quebra: os aceites voltam a ir para o
`config.json`, como antes, e o motivo aparece no terminal.

## Limiares de alerta

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
