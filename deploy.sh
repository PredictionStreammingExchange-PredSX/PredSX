#!/bin/bash

# PredSX Deployment Script
# Usage: ./deploy.sh

echo "🚀 Starting PredSX Deployment..."

# 1. Pull latest changes
echo "📥 Pulling latest code from git..."
git pull origin main

# 2. Check for .env file
if [ ! -f .env ]; then
    echo "⚠️  No .env file found! Creating one from .env.example..."
    cp .env.example .env
    echo "🚨 ACTION REQUIRED: Please edit the .env file with your production secrets before running again."
    exit 1
fi

# 3. Build and Start Services
echo "🏗️  Building and starting containers..."
docker compose -f predsx/deployments/docker-compose.yml up -d --build

# 4. Cleanup
echo "🧹 Cleaning up old images..."
docker image prune -f

echo "✅ Deployment Complete!"
echo "📊 Run 'docker compose -f predsx/deployments/docker-compose.yml ps' to check status."
