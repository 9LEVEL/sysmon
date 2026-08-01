#!/usr/bin/env python3
"""
Testes da trava de instancia unica e da negociacao entre versoes.

Nasceram de um teste no Windows em que o botao de atualizar "nao aparecia".
Nao faltava botao: havia uma instancia ANTIGA rodando, e abrir a nova apenas
trazia a janela da antiga para a frente, em silencio. Quem testava concluia,
com razao, que a versao nova nao tinha a novidade.

O caminho todo e exercitado por socket de verdade, em portas efemeras: e um
protocolo de rede, por menor que seja, e os erros que ele teve nao apareciam
em teste de logica.
"""

from __future__ import annotations

import socket
import sys
import threading
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import sysmon as S  # noqa: E402


def porta_livre() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    porta = s.getsockname()[1]
    s.close()
    return porta


class TestComparacaoDeVersao(unittest.TestCase):
    def test_compara_como_numero(self):
        self.assertTrue(S._mais_novo("4.10.0", "4.9.0"))
        self.assertTrue(S._mais_novo("4.2.1", "4.2.0"))
        self.assertFalse(S._mais_novo("4.2.0", "4.2.0"))
        self.assertFalse(S._mais_novo("4.1.0", "4.2.0"))

    def test_texto_sem_numero_fica_no_chao(self):
        self.assertEqual(S._num("anterior"), (0,))
        self.assertTrue(S._mais_novo("1.0.0", "anterior"))


class TestInstanciaUnica(unittest.TestCase):
    def setUp(self):
        self.abertos: list[S._InstanciaUnica] = []

    def tearDown(self):
        for i in self.abertos:
            i.fechar()

    def nova(self, porta: int) -> S._InstanciaUnica:
        i = S._InstanciaUnica(porta)
        self.abertos.append(i)
        return i

    def test_segunda_instancia_nao_pega_a_porta(self):
        dona = self.nova(porta_livre())
        self.assertTrue(dona.adquirir())
        self.assertFalse(self.nova(dona.porta).adquirir(),
                         "duas instancias segurando a mesma porta")

    def test_banner_diz_a_versao(self):
        dona = self.nova(porta_livre())
        self.assertTrue(dona.adquirir())
        self.assertEqual(self.nova(dona.porta).quem_esta_ai(), S.__version__)

    def test_porta_de_outro_programa_e_reconhecida(self):
        """Servidor que nao fala o nosso banner nao pode ser confundido com um
        sysmon - trocar de porta e o unico caminho, e o usuario precisa saber."""
        srv = socket.socket()
        srv.bind(("127.0.0.1", 0))
        srv.listen(1)
        porta = srv.getsockname()[1]

        def atender():
            try:
                c, _ = srv.accept()
                with c:
                    c.sendall(b"outra coisa\n")
            except OSError:
                pass

        threading.Thread(target=atender, daemon=True).start()
        try:
            self.assertIsNone(self.nova(porta).quem_esta_ai())
        finally:
            srv.close()

    def test_banner_sem_versao_e_tratado_como_anterior(self):
        """Ate a 4.2.0 o banner era so 'sysmon'. Aquelas versoes nao entendem o
        pedido de encerrar, e reconhece-las evita insistir a toa com elas."""
        srv = socket.socket()
        srv.bind(("127.0.0.1", 0))
        srv.listen(1)
        porta = srv.getsockname()[1]

        def atender():
            try:
                c, _ = srv.accept()
                with c:
                    c.sendall(b"sysmon\n")
                    c.recv(16)
            except OSError:
                pass

        threading.Thread(target=atender, daemon=True).start()
        try:
            self.assertEqual(self.nova(porta).quem_esta_ai(), S.ANTIGA)
        finally:
            srv.close()

    def test_pedido_de_sair_chega_no_callback(self):
        dona = self.nova(porta_livre())
        self.assertTrue(dona.adquirir())
        chegou = threading.Event()
        dona.ligar(lambda: None, sair=chegou.set)
        self.assertTrue(self.nova(dona.porta).pedir_para_sair())
        self.assertTrue(chegou.wait(3), "o pedido de sair nao chegou")

    def test_ceder_lugar_libera_a_porta_na_hora(self):
        """A regressao mais cara desta parte.

        O servidor fechava a conexao antes do cliente e, por isso, herdava o
        TIME_WAIT NA PORTA DE CONTROLE. O bind seguinte falhava por perto de um
        minuto: a instancia nova desistia de assumir, avisava de conflito e
        saia - depois de ja ter mandado a antiga encerrar. O usuario ficava sem
        nenhuma das duas rodando.
        """
        dona = self.nova(porta_livre())
        self.assertTrue(dona.adquirir())
        dona.ligar(lambda: None, sair=dona.fechar)

        nova = self.nova(dona.porta)
        self.assertFalse(nova.adquirir())
        self.assertTrue(nova.pedir_para_sair())

        t0 = time.monotonic()
        self.assertTrue(nova.esperar_livre(15),
                        "a porta nao liberou - TIME_WAIT na porta de controle?")
        self.assertLess(time.monotonic() - t0, 5.0,
                        "liberou, mas devagar demais para uma troca de versao")

    def test_servidor_nao_fecha_antes_do_cliente(self):
        """A regra que evita o TIME_WAIT na porta de controle.

        Quem fecha a conexao primeiro herda o TIME_WAIT. Nascendo na porta de
        controle, ele bloqueia o proximo bind por perto de um minuto e a
        instancia nova desiste de assumir o lugar da antiga - foi exatamente
        o que aconteceu, medido com `ss`.

        Testar a corrida em si nao funciona: dentro de um processo so o
        agendamento quase sempre faz o cliente fechar primeiro, e o teste
        passa com o defeito presente. Ja a REGRA e deterministica - se o
        servidor fechar antes, este recv devolve EOF na hora.

        No Linux o SO_REUSEADDR de adquirir() disfarcaria o estrago; o Windows
        nao tem essa saida, porque la SO_REUSEADDR deixaria roubar a porta de
        quem esta escutando e a trava de instancia unica cairia junto.
        """
        dona = self.nova(porta_livre())
        self.assertTrue(dona.adquirir())
        with socket.create_connection(("127.0.0.1", dona.porta), timeout=3) as c:
            self.assertTrue(c.recv(32).startswith(b"sysmon"))
            c.sendall(b"mostrar")
            c.settimeout(1.0)
            try:
                fim = c.recv(16)
            except (TimeoutError, socket.timeout):
                fim = None      # o servidor continua de pe, esperando por nos
            self.assertIsNone(
                fim, "o servidor fechou a conexao antes do cliente e vai "
                     "herdar o TIME_WAIT na porta de controle")


if __name__ == "__main__":
    unittest.main(verbosity=2)
