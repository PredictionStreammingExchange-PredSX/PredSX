@echo off
setlocal enabledelayedexpansion

echo [1/4] Building PredSX Core Images...

REM 1. Discovery
docker build -t predsx-discovery:latest -f services/discovery/Dockerfile .

REM 2. Ingestion
docker build -t predsx-ingestion:latest -f services/ingestion/Dockerfile .

REM 3. Processor
docker build -t predsx-processor:latest -f services/processor/Dockerfile .

REM 4. Storage
docker build -t predsx-storage:latest -f services/storage/Dockerfile .

REM 5. API
docker build -t predsx-api:latest -f services/api/Dockerfile .

echo [2/4] Exporting Images to .tar files...
if not exist "deployments\package" mkdir "deployments\package"

docker save predsx-discovery:latest > deployments\package\discovery.tar
docker save predsx-ingestion:latest > deployments\package\ingestion.tar
docker save predsx-processor:latest > deployments\package\processor.tar
docker save predsx-storage:latest > deployments\package\storage.tar
docker save predsx-api:latest > deployments\package\api.tar

echo [3/4] Preparing Deployment Package...
copy deployments\docker-compose.vps.yml deployments\package\docker-compose.yml
copy deployments\.env deployments\package\.env

echo [4/4] Done!
echo.
echo === SHIPMENT INSTRUCTIONS ===
echo 1. Transfer the 'deployments\package' folder to your VPS.
echo 2. On the VPS, run:
echo    ls *.tar ^| xargs -I {} docker load -i {}
echo 3. Start the stack:
echo    docker-compose up -d
echo =============================
pause
