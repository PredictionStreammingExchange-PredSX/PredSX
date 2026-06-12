@echo off
setlocal enabledelayedexpansion

REM Capture short git commit hash for rollback-friendly image tags
for /f %%i in ('git rev-parse --short HEAD 2^>nul') do set GIT_HASH=%%i
if "!GIT_HASH!"=="" set GIT_HASH=local

echo [1/4] Building PredSX Core Images (git: !GIT_HASH!)...

REM 1. Discovery
docker build -t predsx-discovery:latest -t predsx-discovery:!GIT_HASH! -f services/discovery/Dockerfile .

REM 2. Ingestion
docker build -t predsx-ingestion:latest -t predsx-ingestion:!GIT_HASH! -f services/ingestion/Dockerfile .

REM 3. Processor
docker build -t predsx-processor:latest -t predsx-processor:!GIT_HASH! -f services/processor/Dockerfile .

REM 4. Storage
docker build -t predsx-storage:latest -t predsx-storage:!GIT_HASH! -f services/storage/Dockerfile .

REM 5. API
docker build -t predsx-api:latest -t predsx-api:!GIT_HASH! -f services/api/Dockerfile .

echo [2/4] Exporting Images to .tar files...
if not exist "deployments\package" mkdir "deployments\package"

docker save predsx-discovery:latest -o deployments\package\discovery.tar
docker save predsx-ingestion:latest -o deployments\package\ingestion.tar
docker save predsx-processor:latest -o deployments\package\processor.tar
docker save predsx-storage:latest -o deployments\package\storage.tar
docker save predsx-api:latest -o deployments\package\api.tar

echo [3/4] Preparing Deployment Package...
copy deployments\docker-compose.vps.yml deployments\package\docker-compose.yml
copy deployments\.env deployments\package\.env

echo [4/4] Done!
echo.
echo === SHIPMENT INSTRUCTIONS ===
echo Build tag: !GIT_HASH!
echo.
echo 1. Transfer the 'deployments\package' folder to your VPS.
echo 2. On the VPS, run:
echo    ls *.tar ^| xargs -I {} docker load -i {}
echo 3. Start the stack:
echo    docker-compose up -d
echo.
echo To roll back to this build later:
echo    docker-compose down
echo    REM Edit docker-compose.yml image tags to :!GIT_HASH!
echo    docker-compose up -d
echo =============================
pause
