@echo off
setlocal
set "STABILITY_CLIENT=%~dp0..\..\dist\InternetStability.exe"
if not exist "%STABILITY_CLIENT%" (
  echo InternetStability.exe was not found in the dist folder.
  pause
  exit /b 1
)
start "" "%STABILITY_CLIENT%" %*
