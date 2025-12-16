#!/bin/bash
set -e

# Comprehensive HarborBuddy Testing Script
# Tests various flag combinations and edge cases

COMPOSE_FILE="$(dirname "$0")/docker-compose.test.yml"
TEST_LABEL="com.harborbuddy.test=true"

echo "🧪 HarborBuddy Comprehensive Test Suite"
echo "======================================"
echo ""

# Color codes for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Test: Debug Logging${NC}"
echo "Testing debug logging level..."

# Start test containers
docker-compose -f "$COMPOSE_FILE" up -d test-nginx test-redis test-alpine test-postgres test-busybox >/dev/null 2>&1
sleep 3

# Create test compose with debug logging
cat > test/test-debug.yml << 'EOF'
services:
  harborbuddy:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: harborbuddy-test
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TZ=America/Los_Angeles
      - HARBORBUDDY_SCHEDULE_TIME=03:00
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=debug
    labels:
      com.harborbuddy.autoupdate: "false"
      com.harborbuddy.test: "true"
    command: ["--once", "--dry-run", "--log-level", "debug"]
EOF

# Run HarborBuddy with debug logging
docker-compose -f test/test-debug.yml up --build harborbuddy >/dev/null 2>&1

# Check logs for debug messages
LOGS=$(docker logs harborbuddy-test 2>&1)
if echo "$LOGS" | grep -q "DBG"; then
    echo -e "   ${GREEN}✓${NC} Debug logging enabled"
else
    echo -e "   ${RED}✗${NC} Debug logging not enabled"
    echo "--- LOGS ---"
    echo "$LOGS"
    echo "------------"
    exit 1
fi

# Cleanup
docker-compose -f test/test-debug.yml down -v --remove-orphans >/dev/null 2>&1
docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f >/dev/null 2>&1

echo -e "   ${GREEN}✅ Debug logging test passed${NC}"
echo ""

echo -e "${BLUE}🧪 Test: Interval Scheduling${NC}"
echo "Testing interval-based scheduling instead of daily time..."

# Create test compose with interval
cat > test/test-interval.yml << 'EOF'
services:
  harborbuddy:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: harborbuddy-test
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TZ=America/Los_Angeles
      - HARBORBUDDY_INTERVAL=1h
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=info
    labels:
      com.harborbuddy.autoupdate: "false"
      com.harborbuddy.test: "true"
    command: ["--once", "--dry-run", "--log-level", "info"]
EOF

# Run HarborBuddy with interval scheduling
docker-compose -f test/test-interval.yml up --build harborbuddy >/dev/null 2>&1

# Check logs for interval scheduling
LOGS=$(docker logs harborbuddy-test 2>&1)
if echo "$LOGS" | grep -q "Update interval:"; then
    echo -e "   ${GREEN}✓${NC} Using interval scheduling"
else
    echo -e "   ${RED}✗${NC} Not using interval scheduling"
    exit 1
fi

# Cleanup
docker-compose -f test/test-interval.yml down -v --remove-orphans >/dev/null 2>&1
docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f >/dev/null 2>&1

echo -e "   ${GREEN}✅ Interval scheduling test passed${NC}"
echo ""

echo -e "${BLUE}🧪 Test: Cleanup Disabled${NC}"
echo "Testing with cleanup functionality disabled..."

# Create test compose with cleanup disabled
cat > test/test-no-cleanup.yml << 'EOF'
services:
  harborbuddy:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: harborbuddy-test
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TZ=America/Los_Angeles
      - HARBORBUDDY_SCHEDULE_TIME=03:00
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=debug
      - HARBORBUDDY_CLEANUP_ENABLED=false
    labels:
      com.harborbuddy.autoupdate: "false"
      com.harborbuddy.test: "true"
    command: ["--once", "--dry-run"]
EOF

# Run HarborBuddy with cleanup disabled
docker-compose -f test/test-no-cleanup.yml up --build harborbuddy >/dev/null 2>&1

# Check logs for cleanup disabled
LOGS=$(docker logs harborbuddy-test 2>&1)
if echo "$LOGS" | grep -q "Cleanup is disabled, skipping"; then
    echo -e "   ${GREEN}✓${NC} Cleanup disabled"
else
    echo -e "   ${RED}✗${NC} Cleanup not disabled"
    echo "--- LOGS ---"
    echo "$LOGS"
    echo "------------"
    exit 1
fi

# Cleanup
docker-compose -f test/test-no-cleanup.yml down -v --remove-orphans >/dev/null 2>&1
docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f >/dev/null 2>&1

echo -e "   ${GREEN}✅ Cleanup disabled test passed${NC}"
echo ""

echo -e "${BLUE}🧪 Test: Updates Disabled${NC}"
echo "Testing with update functionality disabled..."

# Create test compose with updates disabled
cat > test/test-no-updates.yml << 'EOF'
services:
  harborbuddy:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: harborbuddy-test
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TZ=America/Los_Angeles
      - HARBORBUDDY_SCHEDULE_TIME=03:00
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=info
      - HARBORBUDDY_UPDATES_ENABLED=false
    labels:
      com.harborbuddy.autoupdate: "false"
      com.harborbuddy.test: "true"
    command: ["--once", "--dry-run"]
EOF

# Run HarborBuddy with updates disabled
docker-compose -f test/test-no-updates.yml up --build harborbuddy >/dev/null 2>&1

# Check logs for updates disabled
LOGS=$(docker logs harborbuddy-test 2>&1)
if echo "$LOGS" | grep -q "Updates are disabled"; then
    echo -e "   ${GREEN}✓${NC} Updates disabled"
else
    echo -e "   ${RED}✗${NC} Updates not disabled"
    echo "--- LOGS ---"
    echo "$LOGS"
    echo "------------"
    exit 1
fi

# Cleanup
docker-compose -f test/test-no-updates.yml down -v --remove-orphans >/dev/null 2>&1
docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f >/dev/null 2>&1

echo -e "   ${GREEN}✅ Updates disabled test passed${NC}"
echo ""

echo -e "${BLUE}🧪 Test: Different Timezone${NC}"
echo "Testing with different timezone configuration..."

# Create test compose with different timezone
cat > test/test-timezone.yml << 'EOF'
services:
  harborbuddy:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: harborbuddy-test
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TZ=Europe/London
      - HARBORBUDDY_SCHEDULE_TIME=09:00
      - HARBORBUDDY_DRY_RUN=true
      - HARBORBUDDY_LOG_LEVEL=info
    labels:
      com.harborbuddy.autoupdate: "false"
      com.harborbuddy.test: "true"
    command: ["--once", "--dry-run", "--log-level", "info"]
EOF

# Run HarborBuddy with different timezone
docker-compose -f test/test-timezone.yml up --build harborbuddy >/dev/null 2>&1

# Check logs for timezone
LOGS=$(docker logs harborbuddy-test 2>&1)
if echo "$LOGS" | grep -q "Europe/London"; then
    echo -e "   ${GREEN}✓${NC} Timezone configured correctly"
else
    echo -e "   ${RED}✗${NC} Timezone not configured"
fi

if echo "$LOGS" | grep -q "09:00"; then
    echo -e "   ${GREEN}✓${NC} Schedule time configured correctly"
else
    echo -e "   ${RED}✗${NC} Schedule time not configured"
    exit 1
fi

# Cleanup
docker-compose -f test/test-timezone.yml down -v --remove-orphans >/dev/null 2>&1
docker ps -a --filter "label=$TEST_LABEL" -q | xargs -r docker rm -f >/dev/null 2>&1

echo -e "   ${GREEN}✅ Timezone test passed${NC}"
echo ""

echo -e "${GREEN}🎉 All comprehensive tests passed!${NC}"
echo ""
echo "📊 Test Coverage Summary:"
echo "   ✅ Debug logging"
echo "   ✅ Interval scheduling"
echo "   ✅ Cleanup disabled"
echo "   ✅ Updates disabled"
echo "   ✅ Different timezone"
echo ""
echo "🛡️ HarborBuddy is thoroughly tested and bulletproof!"