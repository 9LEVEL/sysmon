#!/usr/bin/env python3
"""
Verificacoes estaticas dos scripts do Windows.

Nao ha PowerShell nem cmd na maquina de build, entao estes erros so apareciam
na maquina do usuario, um por vez, a cada release. Sao checagens simples, mas
cobrem exatamente o que ja quebrou de verdade aqui:

  - variavel local com o mesmo nome de um parametro [switch]  (v2.6.0)
  - referencia a arquivo que nao existe no pacote
  - chaves e parenteses desbalanceados

Nao substitui rodar no Windows; evita repetir erro conhecido.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

RAIZ = Path(__file__).resolve().parent.parent
JANELA = RAIZ / "windows-tray"

PS1 = sorted(JANELA.glob("*.ps1"))
BAT = sorted(JANELA.glob("*.bat"))
VBS = sorted(JANELA.glob("*.vbs"))


class TestPowerShell(unittest.TestCase):
    def test_ha_scripts(self):
        self.assertTrue(PS1, "nenhum .ps1 encontrado em windows-tray/")

    def test_variavel_nao_colide_com_parametro(self):
        """PowerShell nao diferencia maiusculas em nome de variavel.

        Atribuir string a uma variavel homonima de um [switch]$X derruba o
        script com ArgumentTransformationMetadataException - foi exatamente o
        que aconteceu com $inicializar vs [switch]$Inicializar.
        """
        for arq in PS1:
            texto = arq.read_text(encoding="utf-8")
            params = re.findall(r"\[(?:switch|int|string|bool)\]\$(\w+)", texto)
            for p in set(params):
                atribuicoes = re.findall(rf"^\s*\${p}\s*=", texto, re.I | re.M)
                self.assertEqual(
                    atribuicoes, [],
                    f"{arq.name}: ${p} e parametro e recebe atribuicao "
                    f"({len(atribuicoes)}x). Renomeie a variavel local.")

    def test_balanceamento(self):
        for arq in PS1 + BAT + VBS:
            texto = arq.read_text(encoding="utf-8")
            # Comentario de linha nao costuma ter chave solta nestes scripts,
            # mas descontamos para nao dar falso positivo em texto de ajuda.
            corpo = "\n".join(l.split("#", 1)[0] if arq.suffix == ".ps1" else l
                              for l in texto.splitlines())
            for abre, fecha in (("{", "}"), ("(", ")")):
                self.assertEqual(
                    corpo.count(abre), corpo.count(fecha),
                    f"{arq.name}: {abre}{fecha} desbalanceado")

    def test_arquivos_referenciados_existem(self):
        """Um .ps1 que chama arquivo inexistente so falha na maquina do usuario."""
        for arq in PS1 + BAT:
            texto = arq.read_text(encoding="utf-8")
            for nome in re.findall(r'"(sysmon\.(?:vbs|bat|pyz))"', texto):
                if nome == "sysmon.pyz":
                    continue   # vem do release, nao do repositorio
                self.assertTrue((JANELA / nome).is_file(),
                                f"{arq.name} referencia {nome}, que nao existe")

    def test_instalador_cobre_os_dois_autostarts(self):
        """Deixar os dois ativos faz duas instancias subirem no logon."""
        texto = (JANELA / "instalar-autostart.ps1").read_text(encoding="utf-8")
        self.assertIn("Register-ScheduledTask", texto)
        self.assertIn("GetFolderPath(\"Startup\")", texto)
        # O caminho escolhido tem que remover o outro.
        self.assertIn("Remove-Item $atalhoInicio", texto)
        self.assertRegex(texto, r"Remover-Tarefa\s+\"sysmon\"")

    def test_desinstalador_limpa_os_dois(self):
        texto = (JANELA / "desinstalar-autostart.ps1").read_text(encoding="utf-8")
        self.assertIn("Startup", texto)
        self.assertIn("Unregister-ScheduledTask", texto)

    def test_limpeza_preserva_config(self):
        """O config.json tem os tokens; apagar seria perder o acesso."""
        texto = (JANELA / "limpar.ps1").read_text(encoding="utf-8")
        self.assertNotRegex(texto, r"Remove-Item[^\n]*config\.json")
        self.assertIn("MANTIDO", texto)


class TestLancadores(unittest.TestCase):
    def test_aplicam_atualizacao_pendente(self):
        """O .pyz so pode ser trocado ANTES do Python abri-lo.

        O sysmon.sh entrou nesta lista na v4.2.0: sem ele o pacote de Linux
        nao tinha ninguem para promover o download, e a atualizacao automatica
        parava no meio do caminho.
        """
        for nome in ("sysmon.vbs", "sysmon.bat", "sysmon.sh"):
            texto = (JANELA / nome).read_text(encoding="utf-8")
            self.assertIn("sysmon-novo.pyz", texto,
                          f"{nome} nao aplica atualizacao pendente")

    def test_lancadores_insistem_na_troca(self):
        """Quando quem pede e o botao, o lancador comeca antes de o processo
        antigo terminar de sair, e a primeira tentativa pega o arquivo em uso.
        No Unix a troca nunca falha por isso, entao so os dois do Windows."""
        for nome in ("sysmon.vbs", "sysmon.bat"):
            texto = (JANELA / nome).read_text(encoding="utf-8").lower()
            self.assertTrue("for " in texto or "loop" in texto,
                            f"{nome} tenta a troca uma vez so")

    def test_sh_repassa_argumentos(self):
        texto = (JANELA / "sysmon.sh").read_text(encoding="utf-8")
        self.assertIn('"$@"', texto, "sysmon.sh engole os argumentos")
        self.assertIn("exec ", texto, "sysmon.sh deixa um shell sobrando")

    def test_bat_pausa_para_o_erro_ficar_visivel(self):
        texto = (JANELA / "sysmon.bat").read_text(encoding="utf-8")
        self.assertIn("pause", texto.lower())

    def test_vbs_sobe_oculto_no_logon(self):
        texto = (JANELA / "sysmon.vbs").read_text(encoding="utf-8")
        self.assertIn("--oculto", texto)


if __name__ == "__main__":
    unittest.main(verbosity=2)
