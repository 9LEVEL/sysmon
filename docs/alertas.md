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

Fica no `config.json`, na chave `reconhecidos` — na raiz, e não dentro de
`alertas`: alertas são a regra, aceitação é a exceção a ela.

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
