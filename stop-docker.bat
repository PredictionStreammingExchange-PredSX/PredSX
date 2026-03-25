@echo off
echo Stopping PredSX Docker Stack...
docker compose -f predsx\deployments\docker-compose.yml down
echo.
echo The stack has been stopped.
echo You can close this window.
pause
