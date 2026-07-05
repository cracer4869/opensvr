@echo off
REM 构建 opensvr 单文件可执行程序（Windows GUI 无控制台窗口）
setlocal
set CGO_ENABLED=0
go build -ldflags "-s -w -H=windowsgui" -o opensvr.exe ./cmd/opensvr
if errorlevel 1 (
    echo build failed
    exit /b 1
)
echo build ok: opensvr.exe
endlocal
