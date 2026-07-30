# Build do sysmon-agent.
#
# O binario e estatico (CGO_ENABLED=0): roda em qualquer Linux com kernel
# compativel, sem depender da libc nem do Python da distribuicao. E o motivo
# de o agente ter saido do Python na v2 - instalar num host novo virou copiar
# um arquivo.

VERSAO  ?= 3.1.0
SAIDA   ?= bin
LDFLAGS := -s -w -X main.versao=$(VERSAO)
GOFLAGS := -trimpath

export CGO_ENABLED = 0

.PHONY: tudo build dist teste vet fmt checagem limpar instalar

tudo: checagem build

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(SAIDA)/sysmon-agent .
	@echo "-> $(SAIDA)/sysmon-agent  ($$(du -h $(SAIDA)/sysmon-agent | cut -f1))"

# Um binario por arquitetura, para levar aos hosts sem Go instalado.
dist:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(SAIDA)/sysmon-agent-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(SAIDA)/sysmon-agent-linux-arm64 .
	@ls -lh $(SAIDA)/

teste:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Falha se algum arquivo estiver fora do formato padrao.
checagem: vet
	@saida=$$(gofmt -l .); \
	if [ -n "$$saida" ]; then echo "gofmt pendente em:"; echo "$$saida"; exit 1; fi
	go test ./...

limpar:
	rm -rf $(SAIDA)

# Compila e instala neste mesmo host. Para instalar em outro, use deploy.sh.
instalar: build
	sudo ./install.sh $(BIND_IP)
