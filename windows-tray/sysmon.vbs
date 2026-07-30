' Inicia o sysmon sem nenhuma janela de console.
' Duplo clique aqui, ou deixe o instalar-autostart.ps1 registrar a tarefa.
Set sh  = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
pasta = fso.GetParentFolderName(WScript.ScriptFullName)
sh.CurrentDirectory = pasta
' --oculto: no login a janela sobe minimizada na bandeja, em vez de pular na
' frente do que voce estava fazendo. Clique no icone para abrir.
sh.Run "pythonw.exe """ & pasta & "\sysmon.pyz"" --oculto", 0, False
