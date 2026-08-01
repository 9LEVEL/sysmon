# Atalhos do projeto inteiro.
#
# Um modulo Go so, com dois binarios: o agente, que roda em cada host Linux
# monitorado, e o cliente, que roda na maquina de onde voce olha. Ate a v5
# eram dois modulos e o contrato do fio existia em duas copias.

VERSAO := $(shell tr -d ' \n' < VERSAO)
LDFLAGS := -s -w -X main.versao=$(VERSAO)

.PHONY: ajuda teste versao agente cliente pacote limpar

ajuda:
	@echo "make teste    - vet, gofmt e testes de tudo"
	@echo "make versao   - confere se todo lugar declara a mesma versao"
	@echo "make agente   - compila o agente para esta arquitetura"
	@echo "make cliente  - compila o cliente para Windows e Linux"
	@echo "make pacote   - gera os pacotes de distribuicao em dist/"
	@echo "make limpar   - remove os binarios e pacotes"

teste:
	@go vet ./...
	@test -z "$$(gofmt -l . )" || { echo "gofmt pendente:"; gofmt -l .; exit 1; }
	@go test ./...
	@echo "== agente com -race =="
	@CGO_ENABLED=1 go test -race ./internal/coleta/ ./cmd/sysmon-agent/

versao:
	@./checar-versao.sh

# O agente e estatico (CGO_ENABLED=0): roda em qualquer Linux com kernel
# compativel, sem depender da libc nem de runtime nenhum.
agente:
	@mkdir -p dist
	@CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o dist/sysmon-agent ./cmd/sysmon-agent
	@ls -lh dist/sysmon-agent

# Cross-compila o cliente do Linux: nenhuma maquina Windows envolvida.
cliente:
	@mkdir -p dist
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
		-ldflags "$(LDFLAGS) -H windowsgui" \
		-o dist/sysmon-windows-amd64.exe ./cmd/sysmon
	@go build -trimpath -ldflags "$(LDFLAGS)" \
		-o dist/sysmon-linux-amd64 ./cmd/sysmon
	@ls -lh dist/sysmon-*amd64*

# Testa antes de empacotar: pacote quebrado nao chega no host de ninguem.
pacote: versao teste
	@./empacotar.sh

limpar:
	rm -rf dist
