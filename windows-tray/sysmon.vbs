' Inicia o sysmon sem janela de console, aplicando atualizacao pendente.
'
' A troca do sysmon.pyz acontece AQUI, antes do Python abrir o arquivo: um
' processo nao consegue sobrescrever com seguranca o proprio .pyz que tem
' aberto. O sysmon baixa para sysmon-novo.pyz; este script promove.
Option Explicit
Dim sh, fso, pasta, atual, novo, args

Set sh  = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

pasta = fso.GetParentFolderName(WScript.ScriptFullName)
sh.CurrentDirectory = pasta

atual = fso.BuildPath(pasta, "sysmon.pyz")
novo  = fso.BuildPath(pasta, "sysmon-novo.pyz")

If fso.FileExists(novo) Then
    On Error Resume Next
    If fso.FileExists(atual) Then fso.DeleteFile atual, True
    fso.MoveFile novo, atual
    ' Se a troca falhar (arquivo em uso), segue com a versao antiga: o
    ' -novo continua la e entra no proximo arranque.
    On Error GoTo 0
End If

' Argumentos extras da linha de comando passam adiante; sem eles, --oculto:
' no logon a janela sobe minimizada na bandeja em vez de pular na frente.
If WScript.Arguments.Count > 0 Then
    Dim i, lista
    lista = ""
    For i = 0 To WScript.Arguments.Count - 1
        lista = lista & " " & WScript.Arguments(i)
    Next
    args = lista
Else
    args = " --oculto"
End If

sh.Run "pythonw.exe """ & atual & """" & args, 0, False
