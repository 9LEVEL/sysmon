# Segurança

## Modelo de ameaça

O agente roda no host do Proxmox, que é a máquina mais crítica do seu ambiente:
comprometê-lo é comprometer todas as VMs e containers. Por isso o projeto é
deliberadamente limitado.

**O que o agente faz:** responde a um `GET`, lê arquivos de `/sys` e `/proc`,
devolve JSON.

**O que ele não faz, por decisão de projeto:**

- não executa `subprocess`, `os.system` nem shell
- não aceita `POST`, `PUT` ou qualquer escrita
- nenhum parâmetro do cliente chega perto do sistema de arquivos
- não usa dependência externa (menos código de terceiros no host)

Se o token vazar, o pior caso é alguém saber a temperatura e o uptime do
servidor. Resista à tentação de adicionar `/exec`, `/reboot` ou `/vm/start` —
é exatamente aí que uma ferramenta caseira vira porta dos fundos.

## Não exponha à internet

O transporte é HTTP puro: o token viaja em texto claro. Nunca abra porta no
roteador. Para acesso remoto, use **WireGuard** ou **Tailscale** e faça bind
na interface do túnel:

```
ExecStart=/usr/bin/python3 /opt/sysmon/sysmon_agent.py --host 100.x.x.x --port 9109
```

O `install.sh` recusa bind em `0.0.0.0` de propósito.

## Firewall

```bash
iptables -A INPUT -p tcp --dport 9109 -s 192.168.0.20 -j ACCEPT
iptables -A INPUT -p tcp --dport 9109 -j DROP
```

Para tornar persistente: `apt install iptables-persistent`.

## Hardening do systemd

A unit aplica `DynamicUser=yes` (usuário efêmero, sem shell, sem home),
`ProtectSystem=strict` (todo o sistema de arquivos read-only para o processo),
`CapabilityBoundingSet=` vazio (nenhuma capability), `SystemCallFilter=@system-service`
e `MemoryDenyWriteExecute=yes`.

Verifique a nota de exposição:

```bash
systemd-analyze security sysmon-agent.service
```

## Isolamento do privilégio

`lvs` precisa de root para ler o thin pool. Em vez de dar root ao agente, um
timer separado roda `lvs` como root a cada 60s e grava o resultado em
`/run/sysmon/thinpool.json`. O agente apenas **lê** esse arquivo. O processo
exposto à rede continua sem poder executar nada.

## Token

Gerado com `openssl rand -hex 24` (192 bits), guardado em `/etc/sysmon/token.env`
com modo `600`. A comparação usa `hmac.compare_digest`, que é de tempo constante,
e respostas `401` têm atraso de 500ms para desencorajar força bruta.

Para rotacionar:

```bash
echo "SYSMON_TOKEN=$(openssl rand -hex 24)" > /etc/sysmon/token.env
chmod 600 /etc/sysmon/token.env
systemctl restart sysmon-agent
```

Atualize o `config.json` no Windows depois.

## Impacto no host

`MemoryMax=64M`, `CPUQuota=5%` e `Nice=10` garantem que um bug no agente jamais
tire recurso das VMs. O cache de 1s evita que múltiplos clientes multipliquem as
leituras de sysfs. Não use intervalo de polling abaixo de 2s — não traz
informação nova e só gera ruído.
