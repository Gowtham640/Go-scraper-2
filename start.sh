#!/usr/bin/env bash
set -e

echo "Starting auth browser service..."

cd auth-browser
node login.js &

echo "Waiting for auth browser to initialize..."
sleep 5

echo "Starting Go server..."

cd ..
go run main.go
