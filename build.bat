@echo off
setlocal enabledelayedexpansion

set BUILD_DIR=bin

:: Get version info
for /f "tokens=*" %%i in ('git describe --tags --always --dirty 2^>nul') do set VERSION=%%i
if not defined VERSION set VERSION=dev

for /f "tokens=*" %%i in ('git rev-parse --short HEAD 2^>nul') do set COMMIT=%%i
if not defined COMMIT set COMMIT=unknown

for /f "tokens=*" %%i in ('powershell -command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ' -AsUTC"') do set BUILD_DATE=%%i
if not defined BUILD_DATE set BUILD_DATE=unknown

set LDFLAGS=-s -w -X 'main.version=%VERSION%' -X 'main.commit=%COMMIT%' -X 'main.buildDate=%BUILD_DATE%'

if "%~1"=="" goto help
goto %~1

:build
    call :build-api
    call :build-worker
    goto end

:build-api
    if not exist %BUILD_DIR% mkdir %BUILD_DIR%
    go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\hermeswa.exe .
    goto end

:build-worker
    if not exist %BUILD_DIR% mkdir %BUILD_DIR%
    go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\worker.exe ./cmd/worker/
    goto end

:clean
    if exist %BUILD_DIR% rmdir /s /q %BUILD_DIR%
    go clean
    goto end

:run
    call :build-api
    %BUILD_DIR%\hermeswa.exe
    goto end

:fmt
    go fmt ./...
    goto end

:vet
    go vet ./...
    goto end

:lint
    call :fmt
    call :vet
    goto end

:help
    echo Available commands:
    echo   build        - Build both API server and worker
    echo   build-api    - Build API server only
    echo   build-worker - Build worker only
    echo   clean        - Remove build artifacts
    echo   run          - Build and run the API server
    echo   fmt          - Format code
    echo   vet          - Run go vet
    echo   lint         - Run fmt and vet
    echo.
    echo Cross-platform builds are handled by GoReleaser (goreleaser-cross).
    goto end

:end
    endlocal
