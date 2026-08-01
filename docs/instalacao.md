# Instalacao detalhada

O caminho curto esta no [README](../README.md). Aqui esta o resto: varios
hosts de uma vez, compilar do codigo, e o que o instalador faz.

## Instalação nos hosts Linux

### Pacote pronto (sem compilar nada)

Os releases trazem o agente empacotado: binário estático, units e instalador.
Não precisa de Go, Python nem nada além de `curl` no host de destino.

```bash
# no host a ser monitorado
curl -fLO https://github.com/9LEVEL/sysmon/releases/latest/download/sysmon-agent-2.4.0-linux-amd64.tar.gz
tar xzf sysmon-agent-2.4.0-linux-amd64.tar.gz
cd sysmon-agent-2.4.0-linux-amd64
sudo ./install.sh 192.168.0.10          # IP da LAN ou do túnel
```

Confira as somas com o `SHA256SUMS` do release.

> **ARM.** A v5.1.0 deixou de publicar o binário de arm64 — não houve nenhum
> pedido, e cada alvo a mais é um binário para conferir e publicar em todo
> release. Compilar você mesmo continua sendo uma linha
> (`GOOS=linux GOARCH=arm64 go build ./cmd/sysmon-agent`), e voltar a publicar
> é uma linha no `empacotar.sh`. **Abra uma issue** se precisar.

### Qual arquivo do release baixar

| Arquivo | Para que serve |
|---|---|
| `sysmon-windows-<v>.zip` | **Windows** — o executável e os scripts de autostart |
| `sysmon-linux-<v>.tar.gz` | **Linux** — o executável |
| `sysmon-agent-<v>-linux-amd64.tar.gz` | agente, em **cada host monitorado** (x86_64) |
| `sysmon-windows-amd64.exe` · `sysmon-linux-amd64` | só o binário — é o que o auto-update baixa |
| `SHA256SUMS` | conferência |

Repare que são coisas diferentes: o **agente** vai nas máquinas Linux que você
quer monitorar; o **cliente** vai na máquina de onde você olha.

Para gerar os pacotes você mesmo: `make pacote` (roda a suíte de testes antes).
E o **`deploy.sh` faz tudo por SSH** — compila, copia e instala em N hosts, sem
passar por release nenhum.

### Vários hosts de uma vez (recomendado)

Na sua máquina, com Go e acesso SSH por chave aos hosts:

```bash
cd linux-agent
cat > hosts.conf <<'EOF'
# apelido  destino_ssh          ip_de_bind     porta
pve        root@192.168.0.10    192.168.0.10   9109
nas        root@192.168.0.20    192.168.0.20
vps        root@100.90.1.5      100.90.1.5            # IP do Tailscale
EOF

./deploy.sh hosts.conf
```

Ele compila, copia o binário certo para a arquitetura de cada host, instala,
testa e no fim escreve um `hosts.json` (modo 600) com os tokens de todos —
pronto para os dois clientes. É o passo que antes você fazia à mão.

### Um host só

```bash
make agente                    # gera dist/sysmon-agent
cp dist/sysmon-agent linux-agent/bin/
scp -r linux-agent root@192.168.0.10:/tmp/
ssh root@192.168.0.10 '/tmp/linux-agent/install.sh 192.168.0.10'
```

O instalador copia para `/opt/sysmon`, gera o token em `/etc/sysmon/token.env`,
sobe o serviço, ativa o timer do thin pool **se** houver LVM thin, remove o
grupo `www-data` da unit **se** não for Proxmox, e testa. No fim imprime a URL
e o token.

Temperaturas exigem que o kernel exponha os sensores. O agente funciona sem
isso (`cpu_temp` vem `null`), mas para ter o número:

```bash
apt install lm-sensors
sensors-detect          # responda YES; grava os módulos em /etc/modules
sensors
```

Feche a porta para o resto da rede:

```bash
iptables -A INPUT -p tcp --dport 9109 -s <IP_DO_CLIENTE> -j ACCEPT
iptables -A INPUT -p tcp --dport 9109 -j DROP
```

## Adicionar mais hosts depois

São dois passos: instalar o agente no host novo e acrescentá-lo ao config dos
clientes. Nada precisa ser reiniciado nos hosts que já funcionam.

**Com `deploy.sh`** — acrescente a linha ao `hosts.conf` e rode de novo. Ele é
idempotente: nos hosts já instalados o token existente é mantido, e o
`hosts.json` sai regenerado com todo mundo.

```bash
cd linux-agent
echo "novo   root@192.168.0.30   192.168.0.30" >> hosts.conf
./deploy.sh hosts.conf
```

**Na mão** — instale no host novo, anote a URL e o token que o `install.sh`
imprime, e acrescente ao `config.json` dos clientes:

```json
{
  "hosts": [
    {"nome": "pve", "url": "http://192.168.0.10:9109/metrics", "token": "..."},
    {"nome": "nas", "url": "http://192.168.0.20:9109/metrics", "token": "..."},
    {"nome": "novo", "url": "http://192.168.0.30:9109/metrics", "token": "..."}
  ]
}
```

Regras do `nome`: precisa ser único (é a chave em toda a interface) e é o que
aparece na tabela, no menu do tray e nos alertas. Se você omitir, ele é
derivado do host da URL.

Depois de editar, reinicie o tray (menu → **Sair**, e abra de novo) — o
`config.json` é lido uma vez, no arranque. O terminal basta rodar de novo.

Confira antes de mexer no tray:

```bash
sysmon term --config config.json --once
```
