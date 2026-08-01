# A interface

Como a janela, o terminal e a bandeja funcionam. Para instalar e comecar,
veja o [README](../README.md).

Tudo roda a partir de **um executável**, **na sua máquina** — sem servidor,
sem banco, sem runtime instalado. Ele busca os dados direto dos agentes
remotos e mostra tudo numa janela nativa (o diagrama no topo mostra o fluxo).

**Windows:** duplo clique em `sysmon.exe`.
**Linux:** `./sysmon`.

A janela abre na **tela de configuração** se ainda não houver `config.json`:
preencha apelido, url e token de cada host, clique em Testar e salve. Não
precisa editar arquivo nenhum.

| Comando | O que faz |
|---|---|
| `sysmon` | janela nativa (+ bandeja no Windows) |
| `sysmon --oculto` | sobe minimizado na bandeja (autostart) |
| `sysmon term` | tabela no terminal, atualiza sozinha |
| `sysmon term --once` | imprime uma vez e sai (script/cron) |
| `sysmon term --json` | estado bruto em JSON |
| `sysmon term --host pve` | só um host |
| `sysmon local` | os sensores **desta** máquina, sem rede nem token (Linux) |
| `sysmon local --once` | idem, uma vez e sai |
| `sysmon --sem-bandeja` · `--sem-update` | desliga o que o nome diz |

O `--once` sai com código útil para script: **0** tudo bem, **1** há alerta,
**2** há host fora do ar. Dá para escrever `sysmon term --once || avisar`.

O **`local`** serve para conferir a ferramenta antes de instalar agente
nenhum, e para um `sysmon local --once` no cron da própria máquina. Ele usa
exatamente o mesmo coletor que o agente usa — até a v4 era um leitor de
`/proc` escrito de novo no cliente, que divergia a cada campo novo.

> **De Python para Go.** Até a v4 o cliente era um `sysmon` de 54 KB que
> exigia Python instalado — e, com ele, um lançador por sistema só para
> conseguir trocar o arquivo durante a atualização. Da v5 em diante é um
> binário de ~13 MB sem dependência nenhuma. Você troca 13 MB de download por
> nunca mais precisar instalar interpretador, e a atualização passou a ser o
> programa se substituindo sozinho.

### O que a série 5 trouxe

| Versão | O quê |
|---|---|
| 5.0 | o cliente vira um executável; fim do Python e dos lançadores |
| 5.1 | [regras de saúde de disco](#saúde-de-disco-smart) com histórico no agente; resposta 64% menor |
| 5.2 | aviso de agente antigo, sempre-no-topo funcionando, gráfico por host e medida, dicas nos ícones |
| 5.3 | [aceitar alertas](#aceitar-um-alerta); janela estreita como tamanho de primeira classe; `ESC` |
| 5.4 | tela de hosts em duas linhas; altura do gráfico como preset |
| 5.5 | recolher host; margem esquerda; processador no cabeçalho; colunas mais justas |

### Atualização automática

**Pelo botão.** O **⭳** no cabeçalho procura versão nova; havendo, baixa e o
botão fica **verde**. Clicar de novo troca o binário e reinicia já na versão
nova. Sem ir ao GitHub, sem descompactar nada.

Sozinho, ele também verifica ao iniciar e a cada 6 horas, e avisa no rodapé
quando há versão pronta.

Três barreiras antes de qualquer troca, porque este é o único código do
projeto que pode fazer estrago sério — ele roda como você, na sua máquina:

- **SHA256** conferido contra o `SHA256SUMS` do release;
- **assinatura do arquivo** (`MZ` no Windows, `ELF` no Linux) — pega download
  truncado e, principalmente, página de erro servida com código 200;
- release **sem o binário da sua plataforma** vira erro explícito, não
  tentativa de instalar o de outra.

A troca em si aproveita uma particularidade do Windows: não dá para
**sobrescrever** um executável em uso, mas dá para **renomear**. Então o atual
vira `.old`, o novo assume o nome, o processo novo sobe e o `.old` some no
arranque seguinte. Se o último passo falhar, o anterior volta — ficar sem
binário nenhum é o pior desfecho possível de uma atualização.

Para desligar: `--sem-update`, ou `"horas_entre_updates": 0` no `config.json`.

### A janela

Sem moldura, escura, monoespaçada. No corpo, **nenhum ícone**: a hierarquia e a
severidade saem de tipografia, alinhamento e cor.

```
 sysmon  1 host · 1 alerta                    ▲ ⭳ 🔔 ⚠ ☰ ⌂ │ ─ ✕

 ⌄ MAQUINA  3G de 15G · Core i3-12100 · Ubuntu   39C · cpu 5% · ram 21%
 DESEMPENHO
   cpu       8 nucleos          ▅▆▁▅▆▁▅▆ ███······              5%
   memoria   3G / 15G           ▁▁▁▁▁▁▁▁ ██········             21%
   carga     5m 0.83 · 15m 0.54                              1.33
 TEMPERATURA
   cpu       critico 100C       ▃▅▂▆▄▃▅▂                       39C
```

No **cabeçalho**, à direita, oito ícones desenhados a vetor — não dependem de
fonte, então aparecem iguais em qualquer sistema:

| Ícone | O que faz |
|---|---|
| **▲** | sempre no topo (acende em azul quando ligado) |
| **⭳** | atualiza o *programa* — verde quando há versão pronta |
| **🔔** | alertas e notificações: aceitar o que você já viu |
| **⚠** | limiares de alerta |
| **☰** | escolher o que aparece, altura do gráfico, margem |
| **⌂** | hosts monitorados, com botão Testar |
| **─ ✕** | minimizar e fechar (vermelho ao passar o mouse) |

Cada um diz o que faz no hover. Não há botão para recarregar os dados: a janela
atualiza sozinha no intervalo, e **F5** força quando você quiser. **ESC** fecha
qualquer diálogo.

**Três colunas com papéis fixos.** O nome nunca é truncado; o detalhe no meio é
quem cede quando você estreita a janela; os números ficam **colados na borda
direita** e acompanham o redimensionamento. A coluna do meio pode ser desligada
em *exibir* — sem ela, o valor encosta na direita e sobra só nome e número.

**A barra de rolagem aparece só quando há o que rolar**, e some quando tudo
cabe na janela.

**No topo, um gráfico animado.** A cor sai da severidade do host que ele mostra:
fica âmbar ou vermelho antes de você ler qualquer número. Anda só com a janela na
tela — recolhida na bandeja, o desenho para, para não gastar CPU da máquina que o
programa existe para vigiar. Se a janela ficar curta demais para os dois, ele se
recolhe sozinho e volta quando há espaço: enfeite não espreme a lista. Pode ser
desligado de vez em *exibir*.

#### O gráfico do topo mostra um host, e você escolhe qual

Duas etiquetas no canto do gráfico dizem o que está desenhado ali — **host** e
**medida** — e clicar em cada uma troca. As medidas são **cpu**, **memória** e
**temperatura**, as três que vivem na mesma escala de 0 a 100.

A **altura** é um preset no `☰`: baixo, médio, alto ou cheio.

A **margem esquerda** (0, 5, 10 ou 20 px), no mesmo lugar, é de onde as seções
partem — `DESEMPENHO` começa nela e as medidas recuam 22 px a partir dali. A
linha do host fica sempre em 0: o fio de estado dela marca onde cada bloco
começa, e afastar isso da borda só estreita a tela. Em 46 px a curva
diz "está vivo"; em 130 px ela mostra a forma, que é o que importa quando você
está acompanhando um pico. Se o preset escolhido não couber junto com a lista,
o gráfico se recolhe sozinho — enfeite não espreme informação.

Até a v5.1 era a média de CPU da frota. Média de hosts diferentes não mede
coisa nenhuma: dois servidores a 10% e a 90% viram 50%, que não descreve nem um
nem outro. E não havia nada dizendo o que o número era.

#### Clique no host para recolher o bloco

Com quatro hosts a árvore inteira cabe na tela; com dez, não cabe nem perto, e
rolar para comparar dois derrota o propósito de ter tudo visível junto. Clicar
na linha do host recolhe o bloco dele — a seta à esquerda diz o estado, e o
cabeçalho continua visível com o resumo. Fica guardado entre sessões.

#### Os ícones do cabeçalho têm dica

Passe o mouse: aparece o que cada um faz. São oito sem rótulo, e a dica existe
justamente para não virar um enigma novo a cada versão.

#### Ela funciona estreita, encostada na lateral

O uso comum é deixar a janela aberta ocupando uns 2/5 da largura da tela, como
um widget sempre à vista. Nessa largura as colunas se ajustam: o detalhe do
meio é cortado antes de encostar no valor, a barra e o sparkline saem quando
não cabem, e os botões dos diálogos passam para uma segunda linha em vez de se
sobreporem.

O mínimo é 470 × 260. **ESC** fecha qualquer diálogo.

Na tela de hosts, cada host ocupa duas linhas — apelido e url em cima, token e
as ações embaixo. Numa linha só eram cinco caixas lado a lado, e em janela
estreita os campos viravam frestas. A ferramenta é feita para **4 a 10 hosts**;
acima disso ela perde o propósito, e com esse teto altura por host é barata.

#### Sparkline

`▅▆▁▅▆▁▅▆` ao lado da barra mostra os últimos ciclos. A barra responde "quanto
agora"; o sparkline responde **"está subindo ou é o normal dele?"** — que é o
que a barra sozinha não conta.

A escala é automática **com piso de amplitude**: oscilar entre 3,0% e 3,2%
continua parecendo o que é (linha reta), mas subir de 3% para 30% preenche o
desenho. Escala fixa 0–100 achataria a variação útil; autoescala pura
transformaria ruído em drama.

**Arrasta pelo cabeçalho**, redimensiona pelo canto inferior direito. O `▲`
prende sobre as outras janelas (Windows e macOS; no X11 e no Wayland o botão
fica só como indicador, porque o toolkit não expõe isso ali).

**Não depende de nada.** A janela é desenhada pelo próprio programa: não há
toolkit do sistema envolvido, e por isso ela sai idêntica no Windows e no
Linux.

### Escolher o que aparece

O `☰` abre a lista de tudo que a ferramenta coleta, agrupado por seção, com
uma caixa para cada campo:

```
☑ RESUMO  na linha do host        ☑ TEMPERATURA
  ☑ temperatura da cpu              ☑ cpu
  ☑ uso de cpu                      ☑ demais sensores do hardware
  ☑ uso de memoria em %           ☑ DISCOS
  ☑ memoria usada em GB             ☑ modelo, temperatura, desgaste, SMART
  ☑ modelo do processador         ☑ ARMAZENAMENTO
  ☑ sistema operacional             ☑ filesystems montados
☑ DESEMPENHO                        ☑ thin pool LVM (Proxmox)
  ☑ uso de cpu                    ☑ REDE
  ☑ memoria                         ☑ interfaces ativas
  ☑ swap
  ☑ carga (load average)
  ☑ tempo no ar
```

A lista mostra **tudo**, mesmo o que você desmarcou — ela serve também de
inventário do que está sendo coletado. Desmarcar uma seção esconde o bloco
inteiro.

O **modelo do processador** aparece na linha do host, ao lado da memória e do
sistema — é identidade da máquina, e não uma medida que muda. Numa janela
estreita ele disputa espaço com os outros dois: desmarque o que não usa.

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

No Windows o sysmon se comporta como qualquer app de bandeja:

- **fechar a janela não encerra o programa**: ele fica no ícone da bandeja, e
  a janela reabre pelo ícone ou pelo atalho
- **Sair** no menu da bandeja é o que encerra de verdade
- o ícone muda de cor conforme o pior host, e um balão avisa quando a
  severidade **sobe** — a cada coleta seria ruído, e "voltou ao normal"
  interromperia sem pedir ação

A bandeja é feita em Win32 puro, sem biblioteca: o ícone é gerado em tempo de
execução, porque a forma é sempre a mesma e só a cor muda.

**No Linux não há bandeja, e é decisão.** O mecanismo varia entre ambientes de
desktop — StatusNotifierItem por DBus nos novos, XEmbed nos antigos — e
nenhum está em toda parte. Uma implementação pela metade seria pior que
nenhuma: o programa pareceria ter sumido ao fechar a janela. Ali, fechar
encerra.
Sem eles, a janela funciona igual — só não há ícone na bandeja.

### Bandeja

Opcional. Sem ela a janela sobe normalmente e o motivo aparece no terminal.

O ícone mostra a temperatura do **host mais quente**, colorido pelo **pior
host**: verde abaixo de 75% do crítico do sensor, amarelo entre 75% e 90%,
vermelho acima, cinza se algum host está offline. Um ponto vermelho no canto
acende para qualquer alerta.

Ao lado da janela nativa o menu é enxuto: **Mostrar janela**, **Sempre no
topo**, atualizar, sair. O overlay do tkinter seria redundante com a janela —
para tê-lo, use `sysmon tray`, que traz o modo antigo com submenu por host,
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

## As cores

A tela toda é monocromática de propósito, e a cor carrega significado. Há
**dois sistemas diferentes** — confundi-los é a causa mais comum de "por que
isso está cinza?".

### 1. Magnitude: quanto está sendo usado

Vale para os **valores** — cpu, memória, swap, filesystem, thin pool — e para a
**barra** ao lado deles. São cinco degraus, e não três:

| Faixa | Cor | Leitura |
|---|---|---|
| < 20% | **cinza apagado** | ocioso — a linha recua da vista |
| 20 – 50% | **branco** | trabalhando, normal |
| 50% até o aviso | **ciano** | notável, mas dentro do esperado |
| ≥ aviso | **âmbar** | atenção |
| ≥ crítico | **vermelho** | agora |

> **É por isso que a cpu às vezes é cinza e às vezes branca.** Não é estado nem
> falha: é o próprio número. Abaixo de 20% ela fica cinza para *sair da frente* —
> num painel com dez hosts, o que está ocioso não deve competir por atenção com
> o que não está. Ao passar de 20% ela vira branca, e a linha "acende" sozinha.
> O mesmo vale para memória, swap e disco.

Os dois últimos degraus são os **limiares configuráveis** (⚠). Âmbar e vermelho
significam sempre *alerta* — por isso a faixa intermediária usa ciano, e não um
amarelo mais claro que competiria com eles.

### 2. Severidade: o estado do host

Vale para a **linha do host**, o fio na borda esquerda dela, o gráfico do topo,
o ícone da bandeja e as linhas do rodapé:

| Cor | Estado |
|---|---|
| **branco** | tudo dentro dos limiares |
| **âmbar** | há aviso |
| **vermelho** | há crítico |
| **cinza** | offline — o host não respondeu |

A linha do host pinta **de ponta a ponta**: um host crítico fica vermelho
inteiro, e não só no nome. É o que permite achar o problema numa frota rolando
a lista de relance.

### As exceções, e por que existem

| Onde | Cor | Por quê |
|---|---|---|
| `5m` / `15m` da carga | **ciano** / **magenta** | separam *duas coisas*, não indicam gravidade — o problema ali é saber qual número é qual |
| nomes das seções | cinza | são rótulos, não dados |
| detalhe do meio | cinza fraco | é contexto; o dado é o número à direita |
| linha do host, meio | azul | identidade da máquina (memória, processador, sistema) |
| alerta aceito, na tela do 🔔 | cinza | continua listado, mas já não pede ação |
| borda dos diálogos | ciano | o mesmo tom do gráfico: "isto está em primeiro plano" |

**Cor nunca carrega sozinha.** O número está sempre do lado, a barra repete a
proporção e o alerta diz em palavras. Quem não distingue âmbar de vermelho
continua lendo a ferramenta inteira.
