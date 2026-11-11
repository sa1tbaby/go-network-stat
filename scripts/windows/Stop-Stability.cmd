@echo off
setlocal
"%~dp0..\..\dist\InternetStability.exe" --pause-monitor
if errorlevel 1 pause
