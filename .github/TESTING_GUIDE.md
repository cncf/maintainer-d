# Testing Guide for Developers

## Quick Start

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run tests with coverage
make test-coverage

# Run specific package tests
make test-package PKG=onboarding

# Run all CI checks locally (before pushing)
make ci-local
```

## Before Pushing Code

**Always run local CI checks:**
```bash
make ci-local
```

This runs the same checks as GitHub Actions:
- ✅ Dependency verification
- ✅ Code formatting check
- ✅ Static analysis (vet, staticcheck)
- ✅ Tests with race detector
- ✅ Coverage report

## Writing Tests

### 1. Use Table-Driven Tests

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"empty string", "", ""},
        {"simple case", "hello", "HELLO"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFunction(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### 2. Use In-Memory Database

```go
func TestWithDatabase(t *testing.T) {
    db := setupTestDB(t)  // Creates :memory: SQLite
    project, maintainers := seedProjectData(t, db)
    
    // Test your code
    result, err := store.GetMaintainersByProject(project.ID)
    require.NoError(t, err)
    assert.Len(t, result, 2)
}
```

### 3. Use Mocks for External APIs

```go
func TestGitHubInteraction(t *testing.T) {
    mockGitHub := NewMockGitHubTransport()
    httpClient := &http.Client{Transport: mockGitHub}
    ghClient := github.NewClient(httpClient)
    
    // Your code that uses GitHub client
    
    // Verify interactions
    comments := mockGitHub.GetCreatedComments()
    assert.Len(t, comments, 1)
}
```

## Running Tests in Different Ways

### By Package
```bash
# Test specific package
cd onboarding && go test -v

# Or from root
go test -v ./onboarding/...
```

### By Function
```bash
# Test specific function
go test -v -run TestFossaChosen ./onboarding/...

# Test specific subtest
go test -v -run TestFossaChosen/successful_onboarding ./onboarding/...
```

### With Different Flags
```bash
# Race detection (always use in CI)
go test -race ./...

# Coverage
go test -cover ./...

# Verbose with race and coverage
go test -v -race -coverprofile=coverage.out ./...

# Short mode (skip slow tests)
go test -short ./...
```

### Watch Mode (Development)
```bash
# Install entr (file watcher)
# Ubuntu/Debian: apt install entr
# macOS: brew install entr

# Auto-run tests on file changes
find . -name "*.go" | entr -c go test ./onboarding/...
```

## Coverage

### View Coverage Report
```bash
# Generate coverage
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Coverage by Package
```bash
# Generate coverage for specific package
cd onboarding
go test -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Debugging Tests

### Print Debug Output
```go
t.Logf("Debug: value = %v", value)  // Only shown with -v or on failure
```

### Run Single Test
```bash
go test -v -run TestSpecificFunction ./package/...
```

### Show All Output
```bash
go test -v ./...  # Shows all t.Log() output
```

### Test with Delve Debugger
```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug a specific test
dlv test ./onboarding -- -test.run TestFossaChosen
```

## Common Test Patterns

### Setup and Teardown
```go
func TestMain(m *testing.M) {
    // Global setup
    os.Exit(m.Run())
    // Global teardown
}

func TestSomething(t *testing.T) {
    // Per-test setup
    cleanup := setupTest(t)
    defer cleanup()
    
    // Test code
}
```

### Parallel Tests
```go
func TestParallel(t *testing.T) {
    t.Parallel()  // Run this test in parallel with others
    
    // Test code
}
```

### Skip Tests
```go
func TestFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping slow test in short mode")
    }
    // Test code
}
```

## Test Organization

```
package/
├── server.go           # Production code
├── server_test.go      # Unit tests for server.go
├── github_mock.go      # Test infrastructure
├── fossa_mock.go       # Test infrastructure
└── test_helpers.go     # Shared test utilities
```

### Naming Conventions
- `*_test.go` - Test files (same package)
- `Test*` - Test functions
- `Benchmark*` - Benchmark functions
- `Example*` - Example/documentation tests

## CI/CD Integration

### What Gets Tested in CI
1. **Dependency verification** - `go mod verify`
2. **Formatting** - `gofmt -s -l .`
3. **Static analysis** - `go vet ./...`
4. **Advanced linting** - `staticcheck ./...`
5. **Tests with race detector** - `go test -race ./...`
6. **Coverage tracking** - Codecov upload

### When Tests Run
- ✅ Every push to main/master/develop
- ✅ Every pull request
- ✅ Can be triggered manually

### Required Checks
For PR merging:
- ✅ All tests pass
- ✅ No linting errors
- ✅ Code formatted correctly
- ✅ No race conditions

## Troubleshooting

### "Tests pass locally but fail in CI"
- Check Go version matches CI
- Run `make ci-local` to simulate CI environment
- Check for timing issues or race conditions

### "Race detector finds issues"
```bash
# Run with race detector locally
go test -race ./...

# Focus on specific package
go test -race ./onboarding/...
```

### "Coverage is low"
```bash
# Find uncovered code
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep -v "100.0%"
```

### "Tests are slow"
```bash
# Identify slow tests
go test -v ./... | grep -E "PASS|FAIL" | grep -E "[0-9]+\.[0-9]+s"

# Profile tests
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof cpu.prof
```

## Best Practices

### ✅ Do
- Write tests for new features
- Use table-driven tests for multiple cases
- Use mocks for external dependencies
- Run `make ci-local` before pushing
- Keep tests fast (< 1s per package)
- Use descriptive test names
- Test error cases, not just happy paths

### ❌ Don't
- Skip running tests before committing
- Test with real external APIs
- Use `time.Sleep()` in tests
- Share state between tests
- Commit untested code
- Ignore race detector warnings

## Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Table Driven Tests](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [Advanced Testing](https://about.sourcegraph.com/blog/go/advanced-testing-in-go)
- [Testify Documentation](https://github.com/stretchr/testify)

## Getting Help

1. Check test output for error messages
2. Run with `-v` for verbose output
3. Check CI logs in GitHub Actions
4. Review `onboarding/TESTING_STRATEGY.md` for patterns
5. Ask the team in Slack/Discord
