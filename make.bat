@echo off
setlocal

::devops 测试环境
set TEST_KODO_HOST=testing.kodo.cloudcare.cn
set TEST_DOWNLOAD_ADDR=cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/test
set TEST_SSL=0
set TEST_PORT=80

::devops 预发环境
set PREPROD_KODO_HOST=preprod-kodo.cloudcare.cn
set PREPROD_DOWNLOAD_ADDR=cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/preprod
set PREPROD_SSL=1
set PREPROD_PORT=443

::alpha 环境
set ALPHA_KODO_HOST=kodo-alpha.cloudcare.cn
set ALPHA_DOWNLOAD_ADDR=cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/alpha
set ALPHA_SSL=1
set ALPHA_PORT=443

::本地搭建的 kodo 测试(XXX: 自行绑定下这个域名到某个地址)
set LOCAL_KODO_HOST=kodo-local.cloudcare.cn
set LOCAL_DOWNLOAD_ADDR=cloudcare-kodo.oss-cn-hangzhou.aliyuncs.com/corsair/local
set LOCAL_SSL=0
set LOCAL_PORT=80

::正式环境
set RELEASE_KODO_HOST=kodo.cloudcare.cn
set RELEASE_DOWNLOAD_ADDR=diaobaoyun-agent.oss-cn-hangzhou.aliyuncs.com/corsair/release
set RELEASE_SSL=1
set RELEASE_PORT=443

set PUB_DIR=pub
set BIN=corsair
set NAME=corsair
set ENTRY=main.go

::set VERSION=$(shell git describe --always --tags)

for /F "delims=" %%i in ('git describe --always --tags') do set VERSION=%%i

echo %VERSION%

mkdir %PUB_DIR% 2>nul 1>&2
::echo %ERRORLEVEL%
::exit /b 2

if "%1%" == "test" goto test
if "%1%" == "release" goto release
if "%1%" == "preprod" goto preprod


:test
echo "===== build test ===="
rd /s /q .\%PUB_DIR%\test 2>nul
md .\build .\%PUB_DIR%\test 2>nul
call:initgit
go run make.go -kodo-host %TEST_KODO_HOST% -binary %BIN% -release test
if %ERRORLEVEL% neq 0 exit /b %ERRORLEVEL%
copy .\env.json .\%PUB_DIR%\test
copy .\fileinfo.json .\%PUB_DIR%\test
::cd .\sign
::sign.bat
exit /b %ERRORLEVEL%

:preprod
echo "===== build preprod ===="
rd /s /q .\%PUB_DIR%\preprod 2>nul
md .\build .\%PUB_DIR%\preprod 2>nul
call:initgit
go run make.go -kodo-host %PREPROD_KODO_HOST% -binary %BIN% -release preprod
if %ERRORLEVEL% neq 0 exit /b %ERRORLEVEL%
copy .\env.json .\%PUB_DIR%\preprod
copy .\fileinfo.json .\%PUB_DIR%\preprod
cd .\sign
sign.bat
exit /b %ERRORLEVEL%

:release
echo "===== build release ===="
rd /s /q .\%PUB_DIR%\release 2>nul
md .\build .\%PUB_DIR%\release 2>nul
call:initgit
go run make.go -kodo-host %RELEASE_KODO_HOST% -binary %BIN% -release release
if %ERRORLEVEL% neq 0 exit /b %ERRORLEVEL%
copy .\env.json .\%PUB_DIR%\release
copy .\fileinfo.json .\%PUB_DIR%\release
cd .\sign
sign.bat
exit /b %ERRORLEVEL%


:initgit
md .\git 2>nul
echo package git > .\git\git.go
echo const ( >> .\git\git.go
echo Sha1 string="" >> .\git\git.go
echo BuildAt string="" >> .\git\git.go
echo Version string="" >> .\git\git.go
echo Golang string="" >> .\git\git.go
echo ) >> .\git\git.go
goto:eof

endlocal