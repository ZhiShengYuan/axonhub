@echo off
setlocal EnableDelayedExpansion

set "REPO=looplj/axonhub"
set "BINARY_NAME=axonclaw"
if not defined INSTALL_DIR set "INSTALL_DIR=."

echo [INFO] AxonClaw Installer
echo [INFO] ==================
echo [INFO] Note: This installer targets the 'axonclaw' component from the AxonHub project.

:: Detect platform
echo [INFO] Detected platform: windows-amd64

:: Get latest version
echo [INFO] Fetching latest version...
for /f "tokens=*" %%a in ('powershell -Command "try { $response = Invoke-RestMethod -Uri 'https://api.github.com/repos/%REPO%/releases/latest' -UseBasicParsing; $response.tag_name } catch { 'latest' }"') do set "VERSION=%%a"

if "%VERSION%"=="" (
    echo [WARNING] Could not determine latest version, using default
    set "VERSION=latest"
)

echo [INFO] Latest version: %VERSION%

:: Resolve asset download URL
echo [INFO] Resolving asset download URL for axonclaw...
set "DOWNLOAD_URL="
for /f "tokens=*" %%a in ('powershell -Command "try { $r = Invoke-RestMethod -Uri 'https://api.github.com/repos/%REPO%/releases/tags/%VERSION%' -UseBasicParsing; $url = ($r.assets | Where-Object { $_.name -match 'axonclaw' -and $_.name -match 'windows' -and $_.name -match 'amd64' } | Select-Object -First 1).browser_download_url; if ($url) { Write-Output $url } else { exit 1 } } catch { exit 1 }"') do set "DOWNLOAD_URL=%%a"

:: Fallback if API failed to find specific asset
if "%DOWNLOAD_URL%"=="" (
    echo [WARNING] API failed or no asset matched; trying constructed URL...
    set "DOWNLOAD_URL=https://github.com/%REPO%/releases/download/%VERSION%/axonclaw-windows-amd64.zip"
)

echo [INFO] Download URL: %DOWNLOAD_URL%

:: Create temp directory
set "TEMP_DIR=%TEMP%\axonclaw-install-%RANDOM%"
mkdir "%TEMP_DIR%" 2>nul

:: Download
echo [INFO] Downloading...
powershell -Command "try { Invoke-WebRequest -Uri '%DOWNLOAD_URL%' -OutFile '%TEMP_DIR%\axonclaw.zip' -UseBasicParsing } catch { exit 1 }"

if errorlevel 1 (
    echo [ERROR] Failed to download binary
    rmdir /s /q "%TEMP_DIR%" 2>nul
    exit /b 1
)

echo [SUCCESS] Download completed

:: Extract
echo [INFO] Extracting to %INSTALL_DIR%...
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"

powershell -Command "try { Expand-Archive -Path '%TEMP_DIR%\axonclaw.zip' -DestinationPath '%INSTALL_DIR%' -Force } catch { exit 1 }"

if errorlevel 1 (
    echo [ERROR] Failed to extract archive
    rmdir /s /q "%TEMP_DIR%" 2>nul
    exit /b 1
)

echo [SUCCESS] Extraction completed

:: Cleanup
rmdir /s /q "%TEMP_DIR%" 2>nul

echo [SUCCESS] AxonClaw %VERSION% installed successfully to %INSTALL_DIR%

:: Check environment variables
if defined AXONCLAW_NAME goto :start
if defined AXONCLAW_BASE_URL goto :start
if defined AXONCLAW_API_KEY goto :start
goto :no_start

:start
echo [INFO] Starting axonclaw...
cd /d "%INSTALL_DIR%"
if exist "start.bat" (
    call start.bat
) else (
    echo [WARNING] start.bat not found, axonclaw not started automatically
    echo [INFO] To start manually, run: cd %INSTALL_DIR% ^&^& start.bat
)
goto :end

:no_start
echo [INFO] Environment variables not set, skipping auto-start
echo [INFO] To start axonclaw manually:
echo [INFO]   cd %INSTALL_DIR% ^&^& start.bat
echo [INFO]
echo [INFO] Or with environment variables:
echo [INFO]   set AXONCLAW_NAME=my-agent
echo [INFO]   set AXONCLAW_BASE_URL=http://localhost:8090
echo [INFO]   set AXONCLAW_API_KEY=your-key
echo [INFO]   start.bat

:end
endlocal
