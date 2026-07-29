' Inicia o traymon sem nenhuma janela de console.
' Duplo clique aqui, ou coloque um atalho em shell:startup
Set sh  = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
pasta = fso.GetParentFolderName(WScript.ScriptFullName)
sh.CurrentDirectory = pasta
sh.Run "pythonw.exe """ & pasta & "\traymon.py""", 0, False
