package main

import (
	"fmt"
	"os"
	"time"

	"sysmon/internal/banco"
	"sysmon/internal/nucleo"
)

// abrirBanco abre o banco ao lado do config e poe os aceites dele na frota.
//
// Na primeira vez traz os aceites que um config.json antigo ainda tenha, e
// os tira do arquivo - so depois de gravados no banco. Se o banco nao abrir,
// nada se perde: a frota segue com os aceites do arquivo, como ate a v5.7, e
// o motivo vai para o stderr.
//
// Le o config de novo do disco em vez de usar o que esta em memoria: o
// `term --host x` filtra os hosts e o `--intervalo` muda o intervalo, e
// regravar aquela config apagaria do arquivo os outros hosts.
func abrirBanco(caminhoConfig string, f *nucleo.Frota) *banco.Banco {
	b, err := banco.Abrir(banco.CaminhoPadrao(caminhoConfig))
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: banco indisponivel (%v); os aceites "+
			"ficam no config.json\n", err)
		return nil
	}
	cfg, errCfg := nucleo.CarregarConfig(caminhoConfig)
	if _, legado := cfg.Bruto["reconhecidos"]; errCfg == nil && legado {
		agora := float64(time.Now().Unix())
		if _, err := b.Importar(cfg.Limiares.Reconhecidos, agora); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: nao consegui importar os aceites "+
				"do config.json (%v)\n", err)
			b.Fechar()
			return nil
		}
		// Importados (ou o banco ja os tinha): o arquivo deixa de ser fonte.
		bruto := cfg.ComoBruto()
		delete(bruto, "reconhecidos")
		if err := nucleo.SalvarConfig(caminhoConfig, bruto); err != nil {
			// Nao e grave: na proxima abertura o Importar ve o historico e
			// nao importa de novo, e a remocao e tentada outra vez.
			fmt.Fprintf(os.Stderr, "aviso: nao consegui tirar os aceites "+
				"do config.json (%v)\n", err)
		}
	}
	rec, err := b.Reconhecidos()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: nao consegui ler os aceites (%v)\n", err)
		b.Fechar()
		return nil
	}
	f.DefinirReconhecidos(rec)
	return b
}
