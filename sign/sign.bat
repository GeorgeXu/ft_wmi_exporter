@echo off
setlocal
.\signtool.exe sign /v /f .\sign.pfx /p Jgy123qaz /t http://timestamp.wosign.com/timestamp ..\build\bin\corsair.exe
endlocal
