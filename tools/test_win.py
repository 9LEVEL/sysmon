#!/usr/bin/env python3
"""
Testes da janela nativa que nao precisam abrir janela.

Tkinter exige display, entao aqui ficam as partes puras: o catalogo de campos,
a decisao de visibilidade e a barra de texto. O desenho em si e conferido
abrindo a janela num display virtual durante o desenvolvimento.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

import sysmon_win as W

FONTE = Path(W.__file__).read_text(encoding="utf-8")


class TestBarra(unittest.TestCase):
    def test_proporcao(self):
        self.assertEqual(W.barra(0, 10), "·" * 10)
        self.assertEqual(W.barra(100, 10), "█" * 10)
        self.assertEqual(W.barra(50, 10), "█████·····")
        self.assertEqual(len(W.barra(37, 10)), 10)

    def test_sem_leitura_nao_vira_zero(self):
        # Barra vazia significa "nao medido", nao "zero por cento" - por isso
        # nada de bloco cheio quando o valor e None.
        self.assertEqual(W.barra(None, 6), "·" * 6)

    def test_fora_da_escala_nao_estoura(self):
        self.assertEqual(len(W.barra(150, 8)), 8)
        self.assertEqual(len(W.barra(-20, 8)), 8)


class TestCatalogo(unittest.TestCase):
    def chaves(self) -> set[str]:
        ks = set()
        for secao, _nota, itens in W.CATALOGO:
            ks.add(f"sec:{secao}")
            ks.update(k for k, _ in itens)
        return ks

    def test_chaves_unicas(self):
        todas = [k for _, _, itens in W.CATALOGO for k, _ in itens]
        self.assertEqual(len(todas), len(set(todas)), "chave repetida no catalogo")

    def test_todo_campo_conferido_esta_no_catalogo(self):
        """Chave usada no desenho mas ausente do catalogo seria inescondivel.

        O usuario veria o campo na tela e nao acharia a caixa para desmarcar,
        sem nenhum erro aparecendo.
        """
        usadas = set(re.findall(r'self\.ver\(([^)]*)\)', FONTE))
        chaves = set()
        for grupo in usadas:
            chaves.update(re.findall(r'"([^"]+)"', grupo))
        faltando = chaves - self.chaves()
        self.assertEqual(faltando, set(),
                         f"chaves usadas no desenho e fora do catalogo: {faltando}")

    def test_todo_campo_do_catalogo_e_usado(self):
        """O contrario tambem engana: caixa que nao desliga nada."""
        usadas = " ".join(re.findall(r'self\.ver\(([^)]*)\)', FONTE))
        for k in self.chaves():
            self.assertIn(f'"{k}"', usadas,
                          f"{k} esta no catalogo mas nao e conferido no desenho")

    def test_secoes_conhecidas_batem_com_as_desenhadas(self):
        desenhadas = set(re.findall(r'secao\("([A-Z]+)"\)', FONTE))
        do_catalogo = {s for s, _, _ in W.CATALOGO if s != "RESUMO"}
        self.assertEqual(desenhadas, do_catalogo)


class TestVisibilidade(unittest.TestCase):
    class Falsa:
        """So a decisao de visibilidade, sem construir a janela."""
        def __init__(self, oculto):
            self.oculto = set(oculto)
        ver = W.Janela.ver

    def test_padrao_mostra_tudo(self):
        j = self.Falsa([])
        self.assertTrue(j.ver("p:cpu"))
        self.assertTrue(j.ver("sec:REDE", "n:todas"))

    def test_esconde_item(self):
        j = self.Falsa(["p:swap"])
        self.assertFalse(j.ver("p:swap"))
        self.assertTrue(j.ver("p:cpu"))

    def test_esconder_a_secao_esconde_o_conteudo(self):
        j = self.Falsa(["sec:REDE"])
        self.assertFalse(j.ver("sec:REDE", "n:todas"))

    def test_campo_novo_aparece_por_padrao(self):
        # Guardamos os OCULTOS: uma versao futura que acrescente campo o mostra,
        # em vez de ele nascer escondido em quem ja tinha config salva.
        j = self.Falsa(["p:swap"])
        self.assertTrue(j.ver("campo:que:ainda:nao:existia"))


class TestAlertasIndependentes(unittest.TestCase):
    def test_esconder_campo_nao_mexe_no_alerta(self):
        """Esconder e escolha de tela; alerta e seguranca.

        Se desmarcar "thin pool" tambem calasse o alerta de thin pool cheio, a
        preferencia de exibicao viraria um jeito silencioso de perder aviso.
        """
        # O resumo/alertas vem de frota.alertas(), que nao consulta self.oculto.
        corpo = FONTE[FONTE.index("def _resumo"):]
        corpo = corpo[:corpo.index("def _tique")]
        self.assertIn("self.frota.alertas()", corpo)
        self.assertNotIn("self.ver(", corpo)
        self.assertNotIn("oculto", corpo)


if __name__ == "__main__":
    unittest.main(verbosity=2)


class TestMagnitude(unittest.TestCase):
    def test_cinco_degraus(self):
        # O ponto: 3% e 30% precisam cair em degraus diferentes. Com so
        # ok/aviso/critico os dois eram a mesma cor e a variacao sumia.
        self.assertEqual(W.magnitude(3, 90, 97), W.M_OCIOSO)
        self.assertEqual(W.magnitude(30, 90, 97), W.M_NORMAL)
        self.assertEqual(W.magnitude(60, 90, 97), W.M_ATIVO)
        self.assertEqual(W.magnitude(92, 90, 97), W.M_AVISO)
        self.assertEqual(W.magnitude(98, 90, 97), W.M_CRITICO)

    def test_sem_leitura_e_ocioso_nao_alerta(self):
        self.assertEqual(W.magnitude(None, 90, 97), W.M_OCIOSO)

    def test_limiar_baixo_nao_pula_degrau(self):
        # Com aviso em 30, um valor de 35 tem que ser AVISO e nao ATIVO -
        # o alerta configurado vence a escala de magnitude.
        self.assertEqual(W.magnitude(35, 30, 40), W.M_AVISO)

    def test_cada_degrau_tem_cor(self):
        for m in (W.M_OCIOSO, W.M_NORMAL, W.M_ATIVO, W.M_AVISO, W.M_CRITICO):
            self.assertIn(m, W.COR_MAG)
        self.assertEqual(len(set(W.COR_MAG.values())), 5, "cores repetidas")


class TestSpark(unittest.TestCase):
    def test_ruido_fica_reto(self):
        # Oscilar entre 3.0 e 3.2 nao e novidade nenhuma; autoescala pura
        # transformaria isso num grafico dramatico.
        self.assertEqual(set(W.spark([3.0, 3.1, 2.9, 3.0, 3.2])), {"▁"})

    def test_mudanca_de_verdade_preenche(self):
        s = W.spark([3, 4, 3, 8, 15, 22, 30, 29])
        self.assertEqual(s[0], "▁")
        self.assertIn(s[-2], "▇█")

    def test_tamanho_igual_a_serie(self):
        self.assertEqual(len(W.spark([1, 2, 3, 4, 5])), 5)

    def test_serie_vazia_ou_so_nulos(self):
        self.assertEqual(W.spark([]), "")
        self.assertEqual(W.spark([None, None]), "")

    def test_nulo_no_meio_nao_quebra(self):
        self.assertEqual(len(W.spark([10, None, 50])), 2)
