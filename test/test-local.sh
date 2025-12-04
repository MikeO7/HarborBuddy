#!/bin/bash
# Local testing script for HarborBuddy

set -e

# Get the project root directory (parent of test/)
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "🧪 HarborBuddy Local Test"
echo "========================="
echo ""

# Step 1: Build binary
echo "📦 Building binary..."
go build -o harborbuddy-test ./cmd/harborbuddy
echo "✓ Built harborbuddy-test"
echo ""

# Step 2: Show version
echo "📋 Version info:"
./harborbuddy-test --version
echo ""

# Step 3: Set up test containers
echo "🐳 Setting up test containers..."
docker run -d --name test-nginx nginx:1.24-alpine >/dev/null 2>&1 || docker start test-nginx >/dev/null 2>&1
docker run -d --name test-redis --label com.harborbuddy.autoupdate=false redis:7-alpine >/dev/null 2>&1 || docker start test-redis >/dev/null 2>&1
echo "✓ Test containers running:"
docker ps --filter "name=test-" --format "  - {{.Names}} ({{.Image}}) {{.Labels}}"
echo ""

# Step 4: Dry-run test
echo "🔍 Testing in dry-run mode..."
echo ""
./harborbuddy-test --once --dry-run --log-level info 2>&1
echo ""
echo "✓ Dry-run completed"
echo ""

# Cleanup prompt
echo "To clean up: docker rm -f test-nginx test-redis"
echo "To test for real: ./harborbuddy-test --once --log-level info"
echo ""
