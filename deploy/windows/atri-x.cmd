@echo off
rem ATRI-X Windows local container command entry point.
rem Equivalent to: powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0atri-x.ps1" %*
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0atri-x.ps1" %*
exit /b %ERRORLEVEL%
