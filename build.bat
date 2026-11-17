@echo off
setlocal EnableExtensions

set "MODE=%~1"
if "%MODE%"=="" set "MODE=release"

:check_go
where go >nul 2>nul
if %errorlevel%==0 goto build

echo Go is not installed or not in PATH.
echo Install Go from https://go.dev/dl/ then press any key to retry, or close this window to cancel.
pause >nul
goto check_go

:build
set "OUT=dist"
if not exist "%OUT%" mkdir "%OUT%"
set "CGO_ENABLED=0"

if /I "%MODE%"=="debug" (
  echo Building debug...
  go build -o "%OUT%\midi-go-debug.exe" .
) else (
  echo Building release...
  go build -trimpath -ldflags "-H=windowsgui -s -w" -o "%OUT%\midi-go-windows64.exe" .
)

if errorlevel 1 (
  echo Build failed.
  exit /b 1
)
echo Build complete: %OUT%
endlocal
