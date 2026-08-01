# Saúde de disco (SMART)

Um SSD ou HD não avisa que vai morrer com um campo dizendo isso. Ele expõe umas
trinta contagens, e a maioria das ferramentas ou repassa a tabela crua — que
ninguém lê — ou reduz tudo a um `PASSED` que só fica `FAILED` quando não há
mais o que fazer.

O `internal/smart` implementa uma especificação de limiares construída sobre
cinco princípios, e cada regra sai de um deles:

1. **Nome, nunca ID.** Atributo de ID 165 a 179 é *vendor-specific*: o mesmo
   170 é `Grown_Bad_Blocks` num WD e `Available Reserved Space` num Intel.
   Casar por número é bug de correção garantido. A chave é o **nome** que o
   `smartctl` resolve pela drivedb dele — e nome fora do catálogo **não vira
   palpite**, é ignorado.
2. **Métrica relativa vence absoluta.** "4 blocos ruins" não quer dizer nada
   sozinho: 4 com 98% de reserva intacta é ruído, 4 com 10% é urgente.
3. **Taxa vence valor absoluto.** 200 setores parados há um ano é um disco
   velho que funciona; 0 → 12 numa semana é um disco morrendo. É por isso que
   o agente guarda histórico (abaixo).
4. **O limiar do fabricante é autoridade.** `VALUE <= THRESH` é falha
   declarada pelo próprio drive, e não há interpretação nossa por cima disso.
5. **Ausência de alerta não é atestado de saúde.** Entre 23% e 36% dos discos
   que falharam não tinham indicador SMART nenhum (Google 2007, Backblaze).
   Por isso a ferramenta **nunca diz "disco saudável"** — diz "sem indicadores
   de falha".

### Três categorias, porque pedem três ações diferentes

| Categoria | O que significa | O que fazer |
|---|---|---|
| **dispositivo** | a mídia está se degradando | trocar o disco |
| **interconexão** | erro de CRC no barramento | trocar o **cabo** ou a porta |
| **host** | desligamentos sujos demais | olhar a **energia** (nobreak) |

Quem mistura as três troca mídia boa e recomeça o ciclo com o mesmo problema —
um cabo SATA ruim produz sintoma em atributo de disco. Por isso o alerta diz
`disco sda (cabo/porta): ...`, e não "troque o disco".

Há também dois **CRÍTICO** distintos: um setor pendente é *aja hoje*; 96% de
vida consumida é *planeje a troca*.

### O histórico, que é o que torna o princípio 3 possível

O `smartctl` só responde sobre o presente. Distinguir "parado" de "crescendo"
exige lembrar do passado, então o **agente** guarda uma série temporal por
`serial` (nunca por `sda`, que vira `sdb` quando alguém troca a ordem dos
cabos) em `/var/lib/sysmon/`, via `StateDirectory=` do systemd.

Fica no agente, e não no cliente, porque o agente é quem tem continuidade: um
histórico que só avança quando alguém está com a janela aberta não serve para
detectar degradação.

A série é densa onde importa e rala no resto — uma amostra por hora nas
últimas 48 h, uma por dia depois disso, 180 dias de retenção. São ~230 pontos
por disco. Contador que **diminui** reinicia a série: contagem SMART só cresce,
e ter caído significa que aquele serial não conta mais a mesma história.

Enquanto não houver histórico suficiente, a regra de taxa fica em **"sem
dados"** — que é diferente de dizer que está tudo bem.

### Ajustando

Tudo isso é configurável pela chave `alertas.smart` do `config.json`, com a
mesma forma da especificação. A herança é **campo a campo**: escrever

```json
{"alertas": {"smart": {"temperature": {"ssd": {"warn": 55}}}}}
```

muda um número e mantém todo o resto do padrão — inclusive os limiares de HDD.

Sem o `smartmontools` instalado no host, o campo vem vazio e o resto funciona.
E disco atrás de controlador RAID, onde o `smartctl` responde mas não alcança
a mídia, vira um estado próprio (**"coleta falhou — saúde desconhecida"**), e
não um disco eternamente sem alerta.
