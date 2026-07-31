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

' A troca insiste por alguns segundos. Quando quem pede a atualizacao e o
' botao da interface, este script comeca antes de o sysmon antigo terminar de
' sair, e o arquivo ainda esta em uso na primeira tentativa. Sem a repeticao,
' clicar em atualizar nao surtiria efeito nenhum ate o proximo logon.
If fso.FileExists(novo) Then
    Dim tentativa
    For tentativa = 1 To 20
        On Error Resume Next
        Err.Clear
        If fso.FileExists(atual) Then fso.DeleteFile atual, True
        If Err.Number = 0 Then fso.MoveFile novo, atual
        If Err.Number = 0 And Not fso.FileExists(novo) Then
            On Error GoTo 0
            Exit For
        End If
        On Error GoTo 0
        WScript.Sleep 300
    Next
    ' Se nem assim, segue com a versao antiga: o -novo continua la e entra
    ' no proximo arranque.
End If

' Sem o Agendador de Tarefas nao ha atraso de inicio configurado, entao a
' espera vem daqui: no logon o Windows ainda esta subindo rede e servicos, e
' comecar a sondar host nesse momento so gera "offline" que se resolve sozinho.
' Nao vale para abertura manual, que passa argumento.
If WScript.Arguments.Count = 0 Then WScript.Sleep 20000

' Argumentos extras da linha de comando passam adiante; sem eles, --oculto:
' no logon a janela sobe minimizada na bandeja em vez de pular na frente.
'
' /agora e consumido aqui e nao repassado: e como o botao de atualizar da
' interface pede "sobe ja, sem a espera de logon e sem minimizar".
If WScript.Arguments.Count > 0 Then
    Dim i, lista, arg
    lista = ""
    For i = 0 To WScript.Arguments.Count - 1
        arg = WScript.Arguments(i)
        If LCase(arg) <> "/agora" Then lista = lista & " " & arg
    Next
    args = lista
Else
    args = " --oculto"
End If

sh.Run "pythonw.exe """ & atual & """" & args, 0, False
