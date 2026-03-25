# Docker Commands for PredSX

Here are the manual commands to manage your local Docker stack. Run these from the root `PredSX` folder (`C:\Users\vijay\OneDrive\Desktop\PredSX`).

## Start the Services
This brings up the database, redis, kafka, and builds/starts all Go microservices in the background:
```bash
docker compose -f predsx/deployments/docker-compose.yml up --build -d
```

## Stop the Services
This safely shuts down all containers and removes them (your database data is persisted safely in volumes):
```bash
docker compose -f predsx/deployments/docker-compose.yml down
```

## View Live Logs
To watch what the microservices are doing in real-time:
```bash
# View all logs
docker compose -f predsx/deployments/docker-compose.yml logs -f

# View logs for a specific service (e.g. streaming component)
docker logs -f deployments-stream-1
```

*(Note: I have also created `start-docker.bat` and `stop-docker.bat` in this folder for your convenience. You can simply double-click those instead of typing the commands!)*
