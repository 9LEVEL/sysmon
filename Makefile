# Atalhos do projeto inteiro. O build do agente fica em linux-agent/Makefile.

.PHONY: ajuda teste teste-go teste-cliente versao build dist cliente pacote limpar

ajuda:
	@echo "make teste    - roda todos os testes (agente + cliente)"
	@echo "make versao   - confere se todo modulo declara a mesma versao"
	@echo "make build    - compila o agente para esta arquitetura"
	@echo "make dist     - compila o agente para amd64 e arm64"
	@echo "make cliente  - compila o cliente para Windows e Linux"
	@echo "make pacote   - gera os tarballs de distribuicao em dist/"
	@echo "make limpar   - remove os binarios e pacotes"

teste: teste-go teste-cliente

teste-go:
	@echo "== agente (Go) =="
	@$(MAKE) -C linux-agent checagem

teste-cliente:
	@echo "== cliente (Go) =="
	@cd cliente && go vet ./... && go test ./...

versao:
	@./checar-versao.sh

build:
	@$(MAKE) -C linux-agent build

dist:
	@$(MAKE) -C linux-agent dist

# Cross-compila o cliente do Linux: nenhuma maquina Windows envolvida.
cliente:
	@mkdir -p dist
	@cd cliente && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -trimpath -ldflags "-s -w -H windowsgui" \
		-o ../dist/sysmon-windows-amd64.exe ./cmd/sysmon
	@cd cliente && go build -trimpath -o ../dist/sysmon-linux-amd64 ./cmd/sysmon
	@ls -lh dist/sysmon-*

# Testa antes de empacotar: pacote quebrado nao chega no host de ninguem.
# A versao vem primeiro por ser instantanea - nao faz sentido rodar a bateria
# inteira para descobrir no fim que um modulo ficou para tras.
pacote: versao teste
	@./empacotar.sh

limpar:
	@$(MAKE) -C linux-agent limpar
	rm -rf dist
