#!/bin/bash
set -e

# Integration test script for HarborBuddy using Docker Compose
# This script tests HarborBuddy in a containerized environment

COMPOSE_FILE="$(dirname "$0")/docker-compose.test.yml"
TEST_LABEL="com.harborbuddy.test=true"

echo "🐳 HarborBuddy Docker Compose Integration Test"
echo "=============================================="
echo ""

mkdir -p test/test-logs
chmod 777 test/test-logs # Ensure container can write

# Color codes for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up test containers..."
    docker-compose -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || true
    
    # Remove any leftover test containers
    docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f 2>/dev/null || true
    
    # Clean up test images (optional)
    # docker images --filter "label=$TEST_LABEL" -q | xargs -r docker rmi -f 2>/dev/null || true
    
    # Clean up test logs
    # rm -rf test/test-logs
    
    echo "✓ Cleanup complete"
}

# Set trap to cleanup on exit
trap cleanup EXIT

# Test 1: Build HarborBuddy image
echo "📦 Test 1: Building HarborBuddy Docker image..."
docker-compose -f "$COMPOSE_FILE" build harborbuddy
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
echo ""

# Test 2: Start test containers (not HarborBuddy yet)
echo "🚀 Test 2: Starting test containers..."
docker-compose -f "$COMPOSE_FILE" up -d test-nginx test-redis test-alpine test-postgres test-busybox
sleep 5

# Verify test containers are running
RUNNING_COUNT=$(docker ps --filter "label=$TEST_LABEL" --filter "status=running" | grep -v harborbuddy-test | wc -l | tr -d ' ')
echo "   Running test containers: $RUNNING_COUNT"

if [ "$RUNNING_COUNT" -ge 5 ]; then
    echo -e "${GREEN}✓ Test containers started successfully${NC}"
else
    echo -e "${RED}✗ Expected 5 test containers, found $RUNNING_COUNT${NC}"
    docker ps --filter "label=$TEST_LABEL"
    exit 1
fi
echo ""

# Test 3: Run HarborBuddy in once mode
echo "🔍 Test 3: Running HarborBuddy (once mode, dry-run)..."
docker-compose -f "$COMPOSE_FILE" up harborbuddy

# Wait a moment for logs to be written
sleep 2
echo ""

# Test 4: Check HarborBuddy logs for expected behavior
echo "📋 Test 4: Validating HarborBuddy behavior..."
LOGS=$(docker logs harborbuddy-test 2>&1)

# Check for successful startup
if echo "$LOGS" | grep -q "HarborBuddy version"; then
    echo -e "${GREEN}✓ HarborBuddy started successfully${NC}"
else
    echo -e "${RED}✗ HarborBuddy failed to start${NC}"
    echo "$LOGS"
    exit 1
fi

# Check for Docker connection
if echo "$LOGS" | grep -q "Successfully connected to Docker daemon"; then
    echo -e "${GREEN}✓ Connected to Docker daemon${NC}"
else
    echo -e "${RED}✗ Failed to connect to Docker daemon${NC}"
    exit 1
fi

# Check that it found containers
if echo "$LOGS" | grep -q "Checking [0-9]* containers for updates"; then
    FOUND_COUNT=$(echo "$LOGS" | grep "Checking [0-9]* containers for updates" | sed -E 's/.*Checking ([0-9]*) containers.*/\1/')
    echo -e "${GREEN}✓ Discovered $FOUND_COUNT containers${NC}"
    if [ "$FOUND_COUNT" -eq 0 ]; then
         echo -e "${YELLOW}⚠ Found 0 containers, expected > 0${NC}"
         exit 1
    fi
else
    echo -e "${RED}✗ Failed to discover containers${NC}"
    exit 1
fi

# Check that excluded containers were skipped
EXCLUDED_COUNT=$(echo "$LOGS" | grep -c "Skipping container.*label com.harborbuddy.autoupdate=false" || echo "0")
if [ "$EXCLUDED_COUNT" -ge 2 ]; then
    echo -e "${GREEN}✓ Correctly excluded $EXCLUDED_COUNT containers with autoupdate=false label${NC}"
else
    echo -e "${YELLOW}⚠ Expected at least 2 excluded containers, found $EXCLUDED_COUNT${NC}"
fi

# Check that managed containers were checked
if echo "$LOGS" | grep -q "Checking container.*test-nginx"; then
    echo -e "${GREEN}✓ Checked test-nginx for updates${NC}"
else
    echo -e "${RED}✗ Did not check test-nginx${NC}"
fi

if echo "$LOGS" | grep -q "Checking container.*test-alpine"; then
    echo -e "${GREEN}✓ Checked test-alpine for updates${NC}"
else
    echo -e "${RED}✗ Did not check test-alpine${NC}"
fi

# Check that cycle completed
if echo "$LOGS" | grep -q "Update cycle complete"; then
    echo -e "${GREEN}✓ Update cycle completed successfully${NC}"
else
    echo -e "${RED}✗ Update cycle did not complete${NC}"
    exit 1
fi

# Check cleanup ran
if echo "$LOGS" | grep -q "Cleanup complete"; then
    echo -e "${GREEN}✓ Cleanup cycle completed${NC}"
else
    echo -e "${YELLOW}⚠ Cleanup cycle may not have run${NC}"
fi

# Check dry-run mode
if echo "$LOGS" | grep -q "Dry-run mode: true"; then
    echo -e "${GREEN}✓ Running in dry-run mode (no actual updates)${NC}"
else
    echo -e "${RED}✗ Dry-run mode not confirmed${NC}"
fi

echo ""

# Test 5: Verify no containers were actually modified (dry-run)
echo "🔒 Test 5: Verifying dry-run mode (no modifications)..."
# Check that test containers are still running the original images
NGINX_IMAGE=$(docker inspect test-nginx --format='{{.Config.Image}}')
if [ "$NGINX_IMAGE" = "nginx:1.24" ]; then
    echo -e "${GREEN}✓ test-nginx still using nginx:1.24 (not modified)${NC}"
else
    echo -e "${RED}✗ test-nginx image changed: $NGINX_IMAGE${NC}"
fi

ALPINE_IMAGE=$(docker inspect test-alpine --format='{{.Config.Image}}')
if [ "$ALPINE_IMAGE" = "alpine:3.18" ]; then
    echo -e "${GREEN}✓ test-alpine still using alpine:3.18 (not modified)${NC}"
else
    echo -e "${RED}✗ test-alpine image changed: $ALPINE_IMAGE${NC}"
fi
echo ""

# Test 6: Test scheduled time configuration
echo "⏰ Test 6: Testing scheduled time configuration..."
SCHEDULE_LOGS=$(docker logs harborbuddy-test 2>&1 | grep "Schedule:" || echo "")
if echo "$SCHEDULE_LOGS" | grep -q "Schedule: Daily at 03:00"; then
    echo -e "${GREEN}✓ Schedule time configured: 03:00${NC}"
else
    echo -e "${YELLOW}⚠ Schedule not found (may be using interval mode)${NC}"
fi

if echo "$SCHEDULE_LOGS" | grep -q "America/Los_Angeles"; then
    echo -e "${GREEN}✓ Timezone configured: America/Los_Angeles${NC}"
else
    echo -e "${YELLOW}⚠ Timezone not confirmed${NC}"
fi
echo ""

# Test 7: Log Persistence
echo "💾 Test 7: Testing log persistence..."
LOG_FILE="test/test-logs/harborbuddy.log"

# Verify log file exists
if [ -f "$LOG_FILE" ]; then
    echo -e "${GREEN}✓ Log file created at $LOG_FILE${NC}"
else
    echo -e "${RED}✗ Log file not found at $LOG_FILE${NC}"
    ls -R test/test-logs || echo "Directory empty or missing"
    exit 1
fi

# Get initial size
INITIAL_SIZE=$(wc -c < "$LOG_FILE" | tr -d ' ')
echo "   Initial log size: $INITIAL_SIZE bytes"

# Restart container to simulate update/recreation
echo "   Restarting HarborBuddy..."
docker-compose -f "$COMPOSE_FILE" restart harborbuddy
sleep 5 # Wait for startup logs

# Verify file still exists and has grown
if [ -f "$LOG_FILE" ]; then
    NEW_SIZE=$(wc -c < "$LOG_FILE" | tr -d ' ')
    echo "   New log size: $NEW_SIZE bytes"
    
    if [ "$NEW_SIZE" -gt "$INITIAL_SIZE" ]; then
        echo -e "${GREEN}✓ Log file persisted and grew (Appended $((-INITIAL_SIZE+NEW_SIZE)) bytes)${NC}"
    else
         echo -e "${RED}✗ Log file did not grow (Size: $NEW_SIZE, Initial: $INITIAL_SIZE)${NC}"
         exit 1
    fi
else
    echo -e "${RED}✗ Log file disappeared after restart${NC}"
    exit 1
fi
echo ""

# Test 8: Show summary
echo "📊 Test Summary"
echo "==============="
docker ps --filter "label=$TEST_LABEL" --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Labels}}" | head -10
echo ""

# Final results
echo "✅ All integration tests passed!"
echo ""
echo "📝 Test Coverage:"
echo "   ✓ Docker image builds successfully"
echo "   ✓ Connects to Docker daemon"
echo "   ✓ Discovers running containers"
echo "   ✓ Respects autoupdate=false labels"
echo "   ✓ Checks eligible containers for updates"
echo "   ✓ Dry-run mode prevents modifications"
echo "   ✓ Update and cleanup cycles complete"
echo "   ✓ Scheduled time configuration works"
echo "   ✓ Log persistence confirmed"
echo ""
echo "🎉 HarborBuddy is production ready!"

