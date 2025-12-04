# HarborBuddy Testing Guide

This directory contains all testing resources for HarborBuddy to help contributors and users test the application.

## Test Files

### Unit Tests
Unit tests are located throughout the codebase in `*_test.go` files:
- `internal/config/config_test.go` - Configuration loading and validation tests
- `internal/updater/updater_test.go` - Container update logic tests
- `internal/cleanup/cleanup_test.go` - Image cleanup tests
- `internal/scheduler/scheduler_test.go` - Scheduler tests

**Run unit tests:**
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...

# Run tests with verbose output
go test -v ./...
```

### Local Testing (`test-local.sh`)
Tests HarborBuddy binary locally with Docker Desktop/Docker Engine.

**Requirements:**
- Docker running locally
- Go installed
- Unix-like environment (macOS/Linux)

**Usage:**
```bash
cd test/
./test-local.sh
```

**What it tests:**
- Builds the HarborBuddy binary
- Creates test containers (nginx, redis)
- Runs HarborBuddy in dry-run mode
- Validates behavior with different container scenarios
- Cleans up test containers

### Docker Compose Integration Tests (`test-docker-compose.sh`)
Complete integration testing using Docker Compose to test HarborBuddy as a containerized application.

**Requirements:**
- Docker with Compose support
- Unix-like environment (macOS/Linux)

**Usage:**
```bash
cd test/
./test-docker-compose.sh
```

**What it tests:**
- Builds HarborBuddy Docker image
- Starts multiple test containers with different configurations
- Runs HarborBuddy in a container with Docker socket mount
- Validates container discovery and exclusion logic
- Tests scheduled time and timezone configuration
- Verifies dry-run mode prevents modifications
- Confirms update and cleanup cycles complete successfully
- Automatically cleans up after testing

**Test containers created:**
- `test-nginx` - Should be managed (no exclusion label)
- `test-redis` - Should be excluded (has `com.harborbuddy.autoupdate=false`)
- `test-alpine` - Should be managed with older version
- `test-postgres` - Should be excluded (database with exclusion label)
- `test-busybox` - Should be managed

### Docker Compose Test Configuration (`docker-compose.test.yml`)
Docker Compose file used by `test-docker-compose.sh` for integration testing.

**Features:**
- Builds HarborBuddy from source
- Configures test environment with TZ and scheduled time
- Creates diverse test container scenarios
- All test containers labeled with `com.harborbuddy.test=true`

## Running Tests in CI/CD

The `.github/workflows/ci.yml` workflow runs automated tests on every push and PR:
- Go unit tests
- Race detection
- Code coverage
- Linting (go vet, go fmt)

## Test Coverage Goals

Current coverage (as of v0.1.0):
- `internal/config`: 81.8%
- `internal/cleanup`: 95.8%
- `internal/updater`: 86.5%
- `internal/scheduler`: 36.7%

## Adding New Tests

When adding new features:

1. **Add unit tests** in the same package as your code:
   ```go
   func TestMyFeature(t *testing.T) {
       // Test implementation
   }
   ```

2. **Update integration tests** if your feature affects:
   - Container discovery
   - Update logic
   - Cleanup behavior
   - Configuration loading
   - Scheduling

3. **Document test scenarios** in this README

## Troubleshooting

### Docker Socket Permission Issues
If you get permission errors accessing `/var/run/docker.sock`:
```bash
# Add your user to the docker group (Linux)
sudo usermod -aG docker $USER
newgrp docker

# Or run tests with sudo (not recommended)
sudo ./test-docker-compose.sh
```

### Test Containers Still Running
If test containers aren't cleaned up automatically:
```bash
# Clean up test containers
docker ps -a --filter "label=com.harborbuddy.test=true" -q | xargs docker rm -f

# Clean up test networks
docker network prune -f
```

### Timezone Test Failures
If timezone tests fail, ensure your Docker image includes timezone data:
- The Dockerfile should copy `/usr/share/zoneinfo` from the builder stage
- Verify with: `docker run --rm harborbuddy ls /usr/share/zoneinfo`

## Manual Testing Scenarios

### Test 1: Scheduled Updates
```bash
# Run HarborBuddy with scheduled time
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e TZ=America/Los_Angeles \
  -e HARBORBUDDY_SCHEDULE_TIME=03:00 \
  -e HARBORBUDDY_DRY_RUN=true \
  harborbuddy-harborbuddy --once
```

### Test 2: Interval-Based Updates
```bash
# Run HarborBuddy with 5-minute interval
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e HARBORBUDDY_INTERVAL=5m \
  -e HARBORBUDDY_DRY_RUN=true \
  harborbuddy-harborbuddy --once
```

### Test 3: Exclusion Labels
```bash
# Start a container that should be excluded
docker run -d --name excluded-test \
  --label com.harborbuddy.autoupdate=false \
  nginx:latest

# Run HarborBuddy and verify it skips the excluded container
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  harborbuddy-harborbuddy --once --dry-run
```

## Contributing Tests

When contributing:
1. Ensure all existing tests pass
2. Add tests for new features
3. Update this README with new test scenarios
4. Follow the existing test patterns and structure

## Questions or Issues?

If you encounter testing issues:
1. Check the [CONTRIBUTING.md](../CONTRIBUTING.md) guide
2. Review existing test files for examples
3. Open an issue on GitHub with test output

