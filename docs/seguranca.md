# Segurança

## Modelo de ameaça

O agente roda em hosts críticos — no caso do Proxmox, comprometê-lo é
comprometer todas as VMs e containers. Por isso o projeto é deliberadamente
limitado.

**O que o agente faz:** responde a um `GET`, lê arquivos de `/sys`, `/proc` e
`/run/sysmon`, devolve JSON.

**O que ele não faz, por decisão de projeto:**

- não executa `subprocess`, `exec` nem shell — não há caminho de código que
  chame um binário externo
- não aceita `POST`, `PUT` nem qualquer método além de `GET` e `HEAD`
- nenhum parâmetro do cliente chega perto do sistema de arquivos: o único
  valor lido da requisição é o token, e ele só é comparado
- não tem dependência externa — o `go.mod` não lista nenhum módulo de
  terceiros, então a superfície é a stdlib do Go e mais nada

Se o token vazar, o pior caso é alguém saber a temperatura e o uptime do
servidor. Resista à tentação de adicionar `/exec`, `/reboot` ou `/vm/start` —
é exatamente aí que uma ferramenta caseira vira porta dos fundos. Há um teste
(`TestRotaInexistente`) que existe justamente para documentar essa fronteira.

## Não exponha à internet

O transporte é HTTP puro: o token viaja em texto claro. Nunca abra porta no
roteador. Para acesso remoto, use **WireGuard** ou **Tailscale** e faça bind
na interface do túnel:

```
ExecStart=/opt/sysmon/sysmon-agent --host 100.90.1.5 --port 9109 --intervalo 5s
```

O `install.sh` recusa bind em `0.0.0.0` e em `::` de propósito. O binário
aceita, mas registra um aviso no journal — a decisão é sua, o alerta fica.

## Firewall

```bash
iptables -A INPUT -p tcp --dport 9109 -s 192.168.0.20 -j ACCEPT
iptables -A INPUT -p tcp --dport 9109 -j DROP
```

Para tornar persistente: `apt install iptables-persistent`. Em Debian 12+ com
nftables:

```bash
nft add rule inet filter input tcp dport 9109 ip saddr != 192.168.0.20 drop
```

## Hardening do systemd

A unit aplica `DynamicUser=yes` (usuário efêmero, sem shell, sem home),
`ProtectSystem=strict` (todo o sistema de arquivos read-only para o processo),
`CapabilityBoundingSet=` vazio (nenhuma capability), `SystemCallFilter=@system-service`,
`SystemCallArchitectures=native`, `MemoryDenyWriteExecute=yes`,
`RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID` e `UMask=0077`.

`RestrictAddressFamilies` permite `AF_INET`/`AF_INET6` (o HTTP) e `AF_UNIX`,
que é exigido pelo `sd_notify` do watchdog.

O `SupplementaryGroups=www-data` existe só para ler `/etc/pve/.vmlist` e
contar VMs. Em host que não é Proxmox o `install.sh` remove essa linha.

Verifique a nota de exposição:

```bash
systemd-analyze security sysmon-agent.service
```

## Isolamento do privilégio

`lvs` precisa de root para ler o thin pool. Em vez de dar root ao agente, um
timer separado roda `lvs` como root a cada 60s e grava o resultado em
`/run/sysmon/thinpool.json`. O agente apenas **lê** esse arquivo.

Esse padrão é o ponto de extensão do projeto: qualquer coleta que precise de
privilégio ou de executar binário (`zpool status`, `smartctl`,
`systemctl --failed`) segue o mesmo caminho — unit isolada escreve JSON em
`/run/sysmon/`, o agente publica em `extras.<nome>`. O processo exposto à rede
nunca ganha privilégio novo, e adicionar um coletor não exige recompilar nem
reiniciar o agente.

Cada bloco de `extras` carrega `_idade_s`. Isso importa: sem esse carimbo, um
timer morto faria o agente servir o mesmo número indefinidamente, e você
acharia que o thin pool está em 62% enquanto ele encheu semanas atrás.

## Token

Gerado com `openssl rand -hex 24` (192 bits), guardado em `/etc/sysmon/token.env`
com modo `600`. A comparação é feita entre os **digests SHA-256** com
`subtle.ConstantTimeCompare`, de modo que o tempo de resposta não depende nem
do conteúdo nem do comprimento do token enviado. Respostas `401` têm atraso
fixo de 500ms para desencorajar força bruta, e são registradas no journal com
o IP de origem.

Alternativa sem expor o segredo no ambiente do processo — use
`SYSMON_TOKEN_FILE` com `LoadCredential=` do systemd:

```ini
LoadCredential=token:/etc/sysmon/token
Environment=SYSMON_TOKEN_FILE=%d/token
```

Para rotacionar:

```bash
echo "SYSMON_TOKEN=$(openssl rand -hex 24)" > /etc/sysmon/token.env
chmod 600 /etc/sysmon/token.env
systemctl restart sysmon-agent
```

Atualize o `config.json` dos clientes depois. Com vários hosts, é mais simples
rodar o `deploy.sh` de novo — ele relê os tokens e regenera o arquivo.

## Os tokens no lado do cliente

O `hosts.json` guarda os tokens de **todos** os hosts em texto claro. É o
arquivo mais sensível do projeto — quem o tiver, lê a telemetria da frota
inteira.

- O `deploy.sh` já o cria com modo `600`.
- O `sysmon-dash.py` avisa no stderr se o arquivo estiver legível por outros.
- No Windows: `icacls config.json /inheritance:r /grant:r "$env:USERNAME:R"`.
- Cada host tem token próprio, então vazar um não dá acesso aos demais.

## Resistência a abuso na rede

O agente é pequeno, mas fica escutando numa porta:

- **Teto de conexões** (32): o excedente é recusado na hora, não enfileirado.
  Sem isso, abrir conexões e nunca enviar requisição consumiria memória do
  host indefinidamente.
- **Timeouts** de leitura de cabeçalho, leitura, escrita e conexão ociosa —
  uma conexão que abre e não fala é derrubada.
- **`MaxHeaderBytes` de 8 KiB.**
- **A coleta não acontece no caminho da requisição.** Uma goroutine amostra o
  host a cada 5s e as requisições só serializam o resultado pronto. Um cliente
  não consegue induzir carga de I/O no host pedindo `/metrics` em laço — isso
  era possível na v1, em que cada requisição lia o sysfs.

## Impacto no host

`MemoryMax=64M`, `CPUQuota=5%`, `TasksMax=32`, `Nice=10` e
`IOSchedulingClass=idle` garantem que um bug no agente jamais tire recurso das
VMs. `GOMAXPROCS=2` evita que o runtime do Go dimensione o scheduler pelo
número de CPUs do host — num servidor de 64 núcleos isso seria desperdício
puro.

O agente recusa `--intervalo` abaixo de 1s. Não há motivo para amostrar mais
rápido: não traz informação nova e só gera I/O.
