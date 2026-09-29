@echo off
node "%~dp0hdc-stub.cjs" %*
exit /b %ERRORLEVEL%
