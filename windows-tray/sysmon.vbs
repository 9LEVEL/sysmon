' Inicia o sysmon (dashboard web + bandeja) sem nenhuma janela de console.
' Duplo clique aqui, ou deixe o instalar-autostart.ps1 registrar a tarefa.
Set sh  = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
pasta = fso.GetParentFolderName(WScript.ScriptFullName)
sh.CurrentDirectory = pasta
' --nao-abrir: no autostart o browser abrindo sozinho a cada login incomoda.
' Use o item "Abrir dashboard" da bandeja, ou http://127.0.0.1:9110/
sh.Run "pythonw.exe """ & pasta & "\sysmon.pyz"" --nao-abrir", 0, False
