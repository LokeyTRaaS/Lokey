# Testing Guide

Comprehensive guide to the LoKey test suite, including how to run tests, test structure, and best practices.

## Table of Contents

- [Quick Start](#quick-start)
- [Running Tests](#running-tests)
- [Test Structure](#test-structure)
- [Test Coverage by Package](#test-coverage-by-package)
- [Test Types](#test-types)
- [Test Utilities and Patterns](#test-utilities-and-patterns)
- [Writing New Tests](#writing-new-tests)
- [Troubleshooting](#troubleshooting)

## Quick Start

Run all tests with a clean, summarized output:

```bash
task test
```

This will:
- Run all tests across all packages
- Show only failures, errors, and package results (no verbose output)
- Display a summary with total passed/failed/skipped counts
- List all skipped tests

Example output:
```
ok      github.com/lokey/rng-service/pkg/api    0.012s
ok      github.com/lokey/rng-service/pkg/database       (cached)
ok      github.com/lokey/rng-service/pkg/fortuna        (cached)

Test Summary:
  TOTAL: 155/156 tests passed (0 failed, 1 skipped)

  Skipped tests:
    - TestServer_UpdateQueueConfig/valid_config
```

## Running Tests

### Using Task (Recommended)

```bash
# Run all tests with summary
task test

# Run tests for a specific package
go test ./tests/api_test

# Run a specific test
go test ./tests/api_test -run TestServer_GetRandomData

# Run tests with verbose output
go test ./tests/api_test -v

# Run tests with race detection
go test ./tests/api_test -race

# Run tests with coverage
go test ./tests/api_test -cover
```

### Direct Go Commands

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test ./tests/api_test ./tests/database_test

# Run tests with coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run tests with race detector
go test ./... -race

# Run tests in parallel
go test ./... -parallel 4
```

### Test Output Filtering

The `task test` command automatically filters verbose output to show only:
- Package results (`ok` or `FAIL`)
- Individual test failures (`--- FAIL: TestName`)
- Individual test skips (`--- SKIP: TestName`)
- Error messages (lines with `Error:`, `expected`, `got:`)
- Summary statistics

To see full verbose output, use `go test -v` directly.

## Test Structure

The test suite is organized by package, mirroring the source code structure:

```
tests/
├── api_test/
│   ├── server_test.go          # API server endpoint tests
│   └── polling_test.go          # Background polling tests
├── database_test/
│   ├── channel_test.go          # In-memory database tests
│   ├── bolt_test.go             # BoltDB implementation tests
│   └── interface_test.go        # Interface compliance tests
├── fortuna_test/
│   └── fortuna_test.go          # Fortuna PRNG algorithm tests
├── atecc608a_test/
│   └── controller_test.go       # Hardware controller tests
└── integration_test/
    ├── api_integration_test.go      # API integration tests
    └── database_integration_test.go # Database integration tests
```

### Test Naming Conventions

- Test files: `*_test.go` (same directory as source files)
- Test functions: `TestFunctionName` or `TestStruct_MethodName`
- Subtests: `t.Run("subtest_name", func(t *testing.T) { ... })`

## Test Coverage by Package

### tests/api_test/ (54 tests)

**server_test.go** - API endpoint tests:
- `TestNewServer` - Server initialization
- `TestServer_GetQueueConfig` - Queue configuration retrieval
- `TestServer_UpdateQueueConfig` - Queue configuration updates
- `TestServer_GetConsumeConfig` - Consume mode configuration
- `TestServer_UpdateConsumeConfig` - Consume mode updates
- `TestServer_GetRandomData` - Random data retrieval in various formats
- `TestServer_GetStatus` - System status endpoint
- `TestServer_HealthCheck` - Health check endpoint
- `TestServer_MetricsHandler` - Prometheus metrics endpoint
- `TestServer_Swagger` - Swagger documentation endpoint

**polling_test.go** - Background polling tests:
- `TestServer_fetchAndStoreTRNGData` - TRNG data fetching and storage
  - Successful fetch and store
  - HTTP error handling
  - Non-200 status handling
  - Invalid JSON response handling
  - Invalid hex data handling
- `TestServer_fetchAndStoreFortunaData` - Fortuna data fetching and storage
  - Successful fetch and store
  - HTTP error handling
  - Non-200 status handling
- `TestServer_seedFortuna` - Fortuna seeding with TRNG data
  - Successful seeding
  - Controller error handling
  - Fortuna seeding error handling
- `TestServer_pollTRNGService_Cancellation` - Context cancellation for TRNG polling
- `TestServer_pollFortunaService_Cancellation` - Context cancellation for Fortuna polling
- `TestServer_seedFortunaWithTRNG_Cancellation` - Context cancellation for seeding

### tests/database_test/ (59 tests)

**channel_test.go** - In-memory database tests:
- `CircularQueue` tests: `Push`, `Get`, `Size`, `Capacity`, concurrency
- `ChannelDBHandler` tests: `StoreTRNGData`, `GetTRNGData`, `StoreFortunaData`, `GetFortunaData`
- Statistics and health check tests
- Note: `UpdateQueueSizes` is not supported (returns error)

**bolt_test.go** - BoltDB implementation tests:
- `BoltDBHandler` initialization and configuration
- Data storage and retrieval (TRNG and Fortuna)
- Database size and path queries
- Statistics and health checks
- Uses temporary directories for test isolation

**interface_test.go** - Interface compliance tests:
- Verifies both implementations satisfy the `DBHandler` interface
- Tests the factory function `NewDBHandler`
- Tests environment variable-based selection

### tests/fortuna_test/ (36 tests)

**fortuna_test.go** - Fortuna PRNG algorithm tests:
- `TestGenerator_NewGenerator` - Generator initialization
- `TestGenerator_AddRandomEvent` - Entropy pool management
- `TestGenerator_Reseed` - Reseeding with external entropy
- `TestGenerator_ReseedFromPools` - Internal pool reseeding
- `TestGenerator_GenerateRandomData` - Random data generation
- `TestGenerator_AmplifyRandomData` - Data amplification
- `TestGenerator_HealthCheck` - Generator health status
- `TestGenerator_GetLastReseedTime` - Reseed timestamp tracking
- `TestGenerator_GetCounter` - Generation counter

### tests/atecc608a_test/ (7 tests)

**controller_test.go** - Hardware controller tests:
- `TestController_calculateAdafruitCRC` - CRC calculation with test vectors
- Note: Full I2C integration tests require hardware

### tests/integration_test/ (105+ tests)

**api_integration_test.go** - API integration tests:
- `TestAPI_Endpoints` - Tests all API endpoints end-to-end with mock external services
  - Configuration endpoints (GET/PUT queue config, GET/PUT consume config)
  - Data endpoints (POST /data with various formats and pagination)
  - Status endpoints (GET /status, GET /health)
  - Metrics endpoints (GET /metrics, GET /api/v1/metrics)
  - Documentation (GET /swagger/index.html)
  - Tests with both ChannelDBHandler and BoltDBHandler
- `TestAPI_CompleteFlow` - Tests complete data flow: store → retrieve → consume
  - Store TRNG data via polling
  - Store Fortuna data via polling
  - Retrieve data in non-consume mode
  - Enable consume mode
  - Retrieve data in consume mode (verify data is removed)
  - Verify queue statistics
  - Tests with both database implementations
- `TestAPI_StartPolling` - Tests orchestration of all three polling goroutines
  - Verifies TRNG polling, Fortuna polling, and Fortuna seeding work together
  - Tests polling counts are incremented correctly
  - Verifies context cancellation stops all polling
  - Tests with both database implementations
- `TestAPI_CORS` - Tests CORS middleware functionality
  - OPTIONS request returns correct CORS headers
  - GET requests include CORS headers
- `TestAPI_HealthCheckFailures` - Tests health check failure scenarios
  - Controller service down
  - Fortuna service down
  - Both services down
  - All services healthy (verification)
- `TestAPI_MetricsAccuracy` - Validates Prometheus metrics are present
  - TRNG queue metrics
  - Fortuna queue metrics
  - Database metrics

**database_integration_test.go** - Database integration tests:
- `TestDatabaseImplementations` - Tests both database implementations with same test cases
  - Table-driven tests run for both ChannelDBHandler and BoltDBHandler
  - Tests all DBHandler interface methods
  - Verifies known differences (UpdateQueueSizes, GetRNGStatistics, GetDatabasePath)
- `TestDatabaseConsistency` - Verifies consistent behavior between implementations
  - Same data stored returns consistent results
  - Queue capacity/current size matches
  - Statistics structure matches
  - Store/Get operations behave identically
  - Pagination works the same way
  - Consume mode behavior matches

### tests/ (156+ tests)

All tests are located in the `tests/` directory, organized by package:

- **api_test/**: 54 tests (API server and polling unit tests)
- **database_test/**: 59 tests (database implementation unit tests)
- **fortuna_test/**: 36 tests (Fortuna PRNG algorithm tests)
- **atecc608a_test/**: 7 tests (hardware controller tests)
- **integration_test/**: 30+ tests (integration tests)

## Test Types

### Unit Tests

Unit tests test individual components in isolation:
- Located in `tests/` subdirectories (e.g., `tests/api_test/`, `tests/database_test/`)
- Use mocks and stubs for dependencies
- Fast execution (< 1 second per package)
- Examples: `tests/api_test/server_test.go`, `tests/database_test/channel_test.go`

### Integration Tests

Integration tests verify components working together:
- Located in `tests/integration_test/` directory
- Use real implementations (not mocks) for database operations
- Use `httptest.Server` for external service mocking (controller, fortuna)
- Test end-to-end API flows and database implementation consistency
- Examples: `tests/integration_test/api_integration_test.go`, `tests/integration_test/database_integration_test.go`

### Test Isolation

- Each test is independent and can run in parallel
- Database tests use temporary directories/files
- API tests use isolated Prometheus registries (prevents metric conflicts)
- No shared state between tests

## Test Utilities and Patterns

### Prometheus Registry Isolation

Tests use isolated Prometheus registries to prevent metric registration conflicts:

```go
testRegistry := prometheus.NewRegistry()
server := NewServer(db, controllerAddr, fortunaAddr, port, testRegistry)
```

This allows multiple test servers to be created without conflicts.

### Mock HTTP Servers

Integration tests use `httptest.Server` to mock external services:

```go
controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Mock response
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(response)
}))
defer controllerServer.Close()
```

### Test Database Setup

Tests use in-memory `ChannelDBHandler` for speed:

```go
db, _ := database.NewChannelDBHandler("", 10, 20)
```

For BoltDB tests, temporary directories are used:

```go
tmpDir, _ := os.MkdirTemp("", "bolt_test_*")
defer os.RemoveAll(tmpDir)
dbPath := filepath.Join(tmpDir, "test.db")
db, _ := database.NewBoltDBHandler(dbPath, 10, 20)
```

### Table-Driven Tests

Many tests use the table-driven pattern:

```go
tests := []struct {
    name     string
    input    int
    expected int
}{
    {"case1", 1, 2},
    {"case2", 2, 4},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        result := function(tt.input)
        if result != tt.expected {
            t.Errorf("got %d, want %d", result, tt.expected)
        }
    })
}
```

## Writing New Tests

### Adding a New Test

1. **Create test file** (if it doesn't exist): `pkg/package/package_test.go`
2. **Follow naming convention**: `TestStruct_MethodName` or `TestFunctionName`
3. **Use subtests** for multiple scenarios: `t.Run("scenario_name", ...)`
4. **Ensure isolation**: No shared state, use test-specific resources

### Example Test Template

```go
func TestNewFeature(t *testing.T) {
    // Setup
    db, _ := database.NewChannelDBHandler("", 10, 20)
    testRegistry := prometheus.NewRegistry()
    server := NewServer(db, "http://localhost:8081", "http://localhost:8082", 0, testRegistry)
    
    t.Run("success case", func(t *testing.T) {
        // Test implementation
        err := server.NewFeature()
        if err != nil {
            t.Fatalf("Expected no error, got %v", err)
        }
    })
    
    t.Run("error case", func(t *testing.T) {
        // Test error handling
        err := server.NewFeatureWithError()
        if err == nil {
            t.Error("Expected error, got nil")
        }
    })
}
```

### Best Practices

1. **Test both success and failure paths**
2. **Use descriptive test names** that explain what is being tested
3. **Keep tests focused** - one concept per test
4. **Use table-driven tests** for multiple similar cases
5. **Clean up resources** - use `defer` for cleanup
6. **Test edge cases** - empty inputs, nil values, boundary conditions
7. **Use `t.Helper()`** in helper functions to improve error messages

## Troubleshooting

### Tests Fail with "duplicate metrics collector registration attempted"

This error occurs when multiple `api.Server` instances are created without isolated Prometheus registries. Solution: Always pass a test-specific registry:

```go
testRegistry := prometheus.NewRegistry()
server := NewServer(db, addr1, addr2, port, testRegistry)
```

### Tests Fail with "database locked"

Only one process can access a BoltDB file at a time. Ensure:
- Tests use temporary directories
- Tests clean up after themselves
- No other processes are using the test database

### Tests Are Slow

- Use `ChannelDBHandler` for unit tests (in-memory, faster)
- Use `-parallel` flag to run tests concurrently
- Check for unnecessary sleeps or timeouts
- Use `-short` flag to skip long-running tests

### Skipped Tests

Some tests are intentionally skipped:
- `TestServer_UpdateQueueConfig/valid_config` - Skipped for `ChannelDBHandler` (feature not supported)
- Hardware-dependent tests may be skipped when hardware is unavailable

To see why a test is skipped, check the test code for `t.Skip()` calls.

## Test Statistics

Current test suite statistics:
- **Total tests**: 261 tests (260 passing, 1 skipped) across 5 test packages
- **Test files**: 9 test files
- **Coverage**: Run `go test ./tests/... -cover` to see current coverage

### Package Breakdown

- `tests/api_test/`: 54 tests (unit tests)
- `tests/database_test/`: 59 tests (unit tests)
- `tests/fortuna_test/`: 36 tests
- `tests/atecc608a_test/`: 7 tests
- `tests/integration_test/`: 105+ tests (integration tests)

## CI/CD Integration

Tests run automatically in CI/CD:
- **GitHub Actions**: Runs on every push and pull request
- **Test command**: `go test -v -race -coverprofile=coverage.out ./...`
- **Coverage reporting**: Results uploaded to codecov

See `.github/workflows/linter.yml` for CI configuration.

## Related Documentation

- **[Development Guide](development.md)** - Building, formatting, and contributing
- **[Architecture Guide](architecture.md)** - System design and components
- **[API Examples](api-examples.md)** - API usage examples

## References

- [Go Testing Package](https://pkg.go.dev/testing)
- [Go Testing Best Practices](https://golang.org/doc/effective_go#testing)
- [Table-Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [httptest Package](https://pkg.go.dev/net/http/httptest)

