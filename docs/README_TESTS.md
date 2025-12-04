# HarborBuddy Test Suite

## Quick Start

### Run All Tests
```bash
go test ./...
```

### Run with Verbose Output
```bash
go test -v ./...
```

### Check Coverage
```bash
go test -cover ./...
```

### Run Specific Package
```bash
go test -v ./internal/updater
go test -v ./internal/config
go test -v ./internal/cleanup
go test -v ./internal/scheduler
```

## Test Coverage

### By Package

| Package | Coverage | Test File | Test Cases |
|---------|----------|-----------|------------|
| `internal/config` | **97.0%** ✅ | config_test.go | 32 |
| `internal/cleanup` | **95.7%** ✅ | cleanup_test.go | 10 |
| `internal/updater` | **86.2%** ✅ | updater_test.go + decision_test.go | 20 |
| `internal/scheduler` | **62.9%** ⚠️ | scheduler_test.go | 8 |

**Overall**: Excellent coverage with comprehensive test cases

### Why Scheduler is Lower

The scheduler package has lower coverage because it contains the main `Run()` function with complex goroutine orchestration, signal handling, and timing logic that's harder to unit test. The core functionality is still well-tested.

## Test Features

### ✅ Comprehensive Coverage
- All major features tested
- Error scenarios covered
- Edge cases handled
- Both success and failure paths

### ✅ TDD Approach
- Tests written with behavior in mind
- Descriptive test names
- Table-driven tests
- Clear expectations

### ✅ Detailed Logging
Every test includes:
- **Context**: What is being tested
- **Setup**: Test inputs and configuration
- **Expectations**: What should happen
- **Results**: Success with ✓ or failure with details
- **Debug info**: Additional context on failures

### ✅ Mock Infrastructure
- Complete mock Docker client
- Operation recording for verification
- Configurable return values
- Configurable errors
- Thread-safe

### ✅ Fast Execution
- All tests run in ~1.5 seconds
- No external dependencies
- Parallel execution where possible
- Isolated tests

## Example Test Output

### Successful Test
```
=== RUN   TestRunUpdateCycle/container_with_new_image_available
    updater_test.go:232:   Test: Container with outdated image should be updated
    updater_test.go:233:   Containers: 1
    updater_test.go:234:   Dry-run: false
2025-12-03T16:42:53-07:00 INF Starting update cycle
2025-12-03T16:42:53-07:00 INF Found 1 running containers
2025-12-03T16:42:53-07:00 INF Checking container nginx for updates
2025-12-03T16:42:53-07:00 INF New image available for nginx:latest
2025-12-03T16:42:53-07:00 INF Updating container nginx with image nginx:latest
2025-12-03T16:42:53-07:00 INF Container nginx updated successfully
2025-12-03T16:42:53-07:00 INF Update cycle complete: 1 updated, 0 skipped
    updater_test.go:260: ✓ Correct number of pulls: 1
    updater_test.go:269: ✓ Correct number of replacements: 1
--- PASS: TestRunUpdateCycle/container_with_new_image_available (0.00s)
```

### Failed Test (Example)
```
=== RUN   TestRunUpdateCycle/expected_failure
    updater_test.go:232:   Test: Should update eligible containers
    updater_test.go:233:   Containers: 2
    updater_test.go:234:   Dry-run: false
    updater_test.go:256:   Expected 2 image pulls, got 1
    updater_test.go:257:   Pulled images: [nginx:latest]
    updater_test.go:262:   Expected 2 container replacements, got 1
    updater_test.go:263:   Replaced containers: [{OldID: container1 NewID: new1 Name: nginx}]
--- FAIL: TestRunUpdateCycle/expected_failure (0.00s)
```

The failure output immediately shows:
- What was expected (2 pulls, 2 replacements)
- What actually happened (1 pull, 1 replacement)
- Which operations were recorded (nginx only)

## Test Files

### 1. Configuration Tests
**File**: `internal/config/config_test.go` (364 lines)
- Default values
- YAML parsing
- Environment overrides
- Validation rules

**Coverage**: 97.0% ✅

### 2. Cleanup Tests
**File**: `internal/cleanup/cleanup_test.go` (387 lines)
- Image cleanup execution
- Dangling vs unused images
- Age-based filtering
- Error handling

**Coverage**: 95.7% ✅

### 3. Updater Tests
**Files**: 
- `internal/updater/updater_test.go` (340 lines)
- `internal/updater/decision_test.go` (108 lines)

Tests:
- Update cycle execution
- Container eligibility
- Pattern matching
- Error recovery

**Coverage**: 86.2% ✅

### 4. Scheduler Tests
**File**: `internal/scheduler/scheduler_test.go` (295 lines)
- Once mode
- Continuous mode
- Cleanup-only mode
- Graceful cancellation

**Coverage**: 62.9% ⚠️

### 5. Mock Infrastructure
**File**: `internal/docker/mock.go` (203 lines)
- Complete Docker client mock
- Operation recording
- Configurable behavior

## Common Test Patterns

### Table-Driven Tests
```go
tests := []struct {
    name        string
    input       X
    expected    Y
    description string
}{
    {
        name:        "descriptive_name",
        input:       value,
        expected:    result,
        description: "What this tests",
    },
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Logf("  Test: %s", tt.description)
        // test code
    })
}
```

### Using Mock Client
```go
mock := docker.NewMockDockerClient()
mock.Containers = []docker.ContainerInfo{...}
mock.Images = []docker.ImageInfo{...}

// Run code under test
RunUpdateCycle(ctx, config, mock)

// Verify operations
if len(mock.PulledImages) != expected {
    t.Errorf("Expected %d pulls, got %d", expected, len(mock.PulledImages))
}
```

### Error Testing
```go
mock.PullImageError = errors.New("network timeout")

err := RunUpdateCycle(ctx, config, mock)

if err == nil {
    t.Error("Expected error, got nil")
}
```

## CI Integration

Tests run automatically in GitHub Actions on:
- Every push to main
- Every pull request
- Before releases

See `.github/workflows/ci.yml`

## Debugging Tips

### 1. Run Single Test
```bash
go test -v -run TestName ./package
go test -v -run TestName/subtest ./package
```

### 2. See All Logging
```bash
go test -v ./...
```

### 3. Generate Coverage Report
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 4. Check for Race Conditions
```bash
go test -race ./...
```

### 5. Run Specific Package Tests
```bash
go test -v github.com/mikeo/harborbuddy/internal/updater
```

## Test Statistics

- **Test Files**: 5
- **Test Functions**: 14
- **Test Cases**: 70+
- **Lines of Test Code**: ~1,700
- **Execution Time**: ~1.5s
- **Pass Rate**: 100%
- **Average Coverage**: 85%+

## Documentation

For detailed information about the test suite:
- See `TEST_SUMMARY.md` for complete test breakdown
- See `TEST_IMPLEMENTATION_COMPLETE.md` for implementation details
- See individual test files for specific test documentation

## Contributing

When adding new features:

1. Write tests first (TDD)
2. Include detailed logging
3. Test both success and error paths
4. Use table-driven tests
5. Run tests before committing:
   ```bash
   go test ./...
   go test -race ./...
   ```

## Support

If tests are failing:
1. Check the verbose output: `go test -v ./...`
2. Look at the logged test description
3. Check the expected vs actual values
4. Review the mock operations
5. Run just the failing test: `go test -v -run TestName`

---

**Test Suite Quality**: ✅ Excellent  
**Coverage**: ✅ 85%+ Average  
**Maintainability**: ✅ High  
**Debugging**: ✅ Easy with detailed logs

