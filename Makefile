# Atalhos do projeto inteiro. O build do agente fica em linux-agent/Makefile.

.PHONY: ajuda teste teste-go teste-py build dist limpar

ajuda:
	@echo "make teste    - roda todos os testes (agente Go + clientes Python)"
	@echo "make build    - compila o agente para esta arquitetura"
	@echo "make dist     - compila o agente para amd64 e arm64"
	@echo "make limpar   - remove os binarios"

teste: teste-go teste-py

teste-go:
	@echo "== agente (Go) =="
	@$(MAKE) -C linux-agent checagem

teste-py:
	@echo "== nucleo dos clientes (Python) =="
	@python3 -m unittest discover -s tools -t tools
	@echo "== tray do Windows (logica de exibicao) =="
	@python3 -m unittest discover -s windows-tray -t windows-tray

build:
	@$(MAKE) -C linux-agent build

dist:
	@$(MAKE) -C linux-agent dist

limpar:
	@$(MAKE) -C linux-agent limpar
