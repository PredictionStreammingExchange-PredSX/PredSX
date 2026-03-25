@echo off
echo Starting PredSX Docker Stack...
docker compose -f predsx\deployments\docker-compose.yml up --build -d
echo.
echo The stack is now running in the background.
echo You can close this window.
pause
