# CI/CD Best Practices for maintainer-d

## Overview

This document outlines best practices for running tests in CI/CD pipelines for the maintainer-d project.

## Current CI Setup

### GitHub Actions Workflow (`test.yml`)

The CI pipeline consists of three jobs:

1. **`test`** - Main test job with comprehensive checks
2. **`test-by-package`** - Parallel package-specific testing
3. **`lint`** - Code quality and style checks

## Best Practices Implemented

### 1. **Trigger on Appropriate Events**

✅ **What we do:**
```yaml
on:
  push:
    branches: [main, master, develop]
  pull_request:
    branches: [main, master, develop]
```

**Why:** Tests run on pushes to main branches and all PRs, catching issues early.

**Alternative approaches:**
- Add `paths:` to only trigger on specific file changes
- Use `workflow_dispatch:` for manual triggers

### 2. **Use Matrix Strategy for Version Testing**

✅ **What we do:**
```yaml
strategy:
  matrix:
    go-version: ['1.24.x']
```

**Why:** Ensures compatibility across Go versions.

**When to expand:**
```yaml
go-version: ['1.23.x', '1.24.x']  # Test multiple versions
```

### 3. **Dependency Verification**

✅ **What we do:**
```yaml
- run: go mod verify
```

**Why:** Catches corrupted or tampered dependencies early.

**Additional hardening:**
```yaml
- run: go mod download
- run: go mod verify
```

### 4. **Code Quality Checks (Pre-Test)**

✅ **What we do:**
```yaml
- run: go vet ./...      # Static analysis
- run: go fmt check      # Code formatting
- run: staticcheck ./... # Advanced linting
```

**Why:** Catches common bugs and style issues before running tests.

**Order matters:** Run fast checks (fmt, vet) before slower ones (tests).

### 5. **Race Detection**

✅ **What we do:**
```yaml
- run: go test -race ./...
```

**Why:** Catches concurrency bugs (critical for webhook handlers).

**Trade-off:** ~10x slower, but essential for concurrent code.

### 6. **Coverage Tracking**

✅ **What we do:**
```yaml
- run: go test -coverprofile=coverage.out -covermode=atomic ./...
- run: go tool cover -func=coverage.out
```

**Why:** 
- Tracks test coverage over time
- `atomic` mode works with `-race`
- Displays summary in CI logs

**Best practices:**
- Don't enforce 100% coverage (diminishing returns)
- Target 60-80% for critical paths
- Focus on testing business logic, not trivial code

### 7. **Parallel Testing by Package**

✅ **What we do:**
```yaml
strategy:
  fail-fast: false
  matrix:
    package: [db, onboarding, plugins/fossa]
```

**Why:**
- Tests run in parallel (faster CI)
- `fail-fast: false` shows all failures, not just first
- Easier to identify which package has issues

**Trade-off:** Uses more runner minutes but provides faster feedback.

### 8. **Separate Linting Job**

✅ **What we do:**
```yaml
jobs:
  test: ...
  lint: ...
```

**Why:**
- Tests and linting run in parallel
- Failures are clearly categorized
- Can have different retry/failure policies

### 9. **Continue on Error for Non-Critical Steps**

✅ **What we do:**
```yaml
- uses: codecov/codecov-action@v4
  continue-on-error: true
```

**Why:** Coverage upload shouldn't fail the build if Codecov is down.

**Use for:**
- Third-party service uploads
- Notifications
- Optional reports

**Don't use for:**
- Actual tests
- Security checks
- Build steps

### 10. **Caching**

✅ **What we do:**
```yaml
uses: actions/setup-go@v5
with:
  cache: true
```

**Why:** Caches Go modules and build cache, speeding up CI by ~50%.

## Testing Best Practices

### Test Organization

```bash
# Run all tests
go test ./...

# Run specific package
go test ./onboarding/...

# Run specific test
go test -run TestFossaChosen ./onboarding/...

# Verbose output
go test -v ./...
```

### Test Naming Conventions

✅ **Follow Go conventions:**
```go
func TestFunctionName(t *testing.T)           // Unit test
func TestFunctionName_EdgeCase(t *testing.T)  // Specific scenario
func BenchmarkFunctionName(b *testing.B)      // Benchmark
func ExampleFunctionName()                     // Example/doc test
```

### Test Isolation

✅ **Our approach:**
- In-memory SQLite databases (`:memory:`)
- Mock external APIs (GitHub, FOSSA)
- No shared state between tests
- Each test creates fresh fixtures

**Why this is CI-friendly:**
- No external dependencies
- Tests run fast (~20ms)
- No flaky network issues
- Can run in parallel

### Test Data

✅ **What we do:**
```go
// Create test data programmatically
project, maintainers := seedProjectData(t, db)
```

**Avoid:**
- ❌ External test data files
- ❌ Shared test databases
- ❌ Relying on API tokens in CI
- ❌ Tests that hit real APIs

## Advanced CI Patterns

### 1. **Test Splitting for Large Projects**

```yaml
strategy:
  matrix:
    shard: [1, 2, 3, 4]
steps:
  - run: go test -v ./... -shard ${{ matrix.shard }}/4
```

**When to use:** 100+ tests taking >5 minutes.

### 2. **Conditional Steps**

```yaml
- name: Upload coverage
  if: github.event_name == 'push'
  run: ...
```

**Use cases:**
- Only upload coverage on main branch
- Skip slow tests on drafts
- Deploy only from main

### 3. **Required Status Checks**

**GitHub Settings → Branches → Branch Protection:**
- ✅ Require status checks: `test`, `lint`
- ✅ Require branches to be up to date
- ✅ Require pull request reviews

### 4. **Dependency Caching Strategy**

```yaml
- uses: actions/cache@v4
  with:
    path: |
      ~/.cache/go-build
      ~/go/pkg/mod
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    restore-keys: |
      ${{ runner.os }}-go-
```

**Included by default** with `setup-go@v5` `cache: true`.

### 5. **Build Matrix for OS/Arch**

```yaml
strategy:
  matrix:
    os: [ubuntu-latest, macos-latest, windows-latest]
    go: ['1.24.x']
```

**When to use:** Building distributable binaries.

**For testing:** Usually Ubuntu is sufficient unless testing OS-specific code.

## Security Best Practices

### 1. **Minimal Permissions**

✅ **What we do:**
```yaml
permissions:
  contents: read
```

**Why:** Follows principle of least privilege.

### 2. **Secret Management**

```yaml
env:
  CODECOV_TOKEN: ${{ secrets.CODECOV_TOKEN }}
```

**Best practices:**
- Store secrets in GitHub Secrets
- Never commit tokens to code
- Use environment variables
- Scope secrets to necessary steps

### 3. **Dependency Pinning**

```yaml
uses: actions/checkout@v4      # ✅ Pin to major version
uses: actions/setup-go@v5      # ✅ Pin to major version
```

**Why:** Prevents supply chain attacks while getting updates.

**Alternative:** Pin to SHA for maximum security:
```yaml
uses: actions/checkout@8ade135a41bc03ea155e62e844d188df1ea18608  # v4.1.0
```

## Performance Optimization

### Current Performance Baseline

```
Test execution: ~20-50ms per package
Total CI time: ~2-3 minutes including:
  - Checkout: ~5s
  - Go setup: ~10s (with cache)
  - Dependencies: ~5s (cached)
  - Tests: ~1s
  - Linting: ~30s
```

### Optimization Tips

1. **Use test caching** (already enabled)
2. **Run fast checks first** (fmt, vet before tests)
3. **Parallel package testing** (already implemented)
4. **Skip tests on docs changes:**

```yaml
on:
  pull_request:
    paths-ignore:
      - '**.md'
      - 'docs/**'
```

5. **Use GitHub Actions cache effectively:**

```yaml
# Already using setup-go cache: true
# Can add additional caching:
- uses: actions/cache@v4
  with:
    path: ~/.cache/golangci-lint
    key: golangci-lint-${{ hashFiles('.golangci.yml') }}
```

## Monitoring and Alerts

### Coverage Trends

Use Codecov to track:
- Coverage changes per PR
- Coverage trends over time
- Uncovered critical paths

**Setup:**
1. Sign up at codecov.io
2. Add `CODECOV_TOKEN` to GitHub Secrets
3. Coverage uploads automatically (already in workflow)

### Test Flakiness

**Detection:**
```yaml
- name: Run tests multiple times
  run: |
    for i in {1..5}; do
      go test ./... || exit 1
    done
```

**Only use for debugging** - slows CI significantly.

### Failure Notifications

**GitHub settings:**
- Enable email notifications for failed checks
- Use Slack/Discord webhooks for team notifications

```yaml
- name: Notify on failure
  if: failure()
  uses: slackapi/slack-github-action@v1
  with:
    webhook: ${{ secrets.SLACK_WEBHOOK }}
```

## Common Pitfalls to Avoid

### ❌ Don't: Test with Real APIs

```go
// Bad - requires API token, can fail, slow, flaky
func TestRealGitHub(t *testing.T) {
    client := github.NewClient(nil)
    _, _, err := client.Users.Get(ctx, "torvalds")
}
```

✅ **Do: Use mocks** (already implemented)

### ❌ Don't: Shared State Between Tests

```go
// Bad - tests interfere with each other
var sharedDB *gorm.DB

func TestA(t *testing.T) { sharedDB.Create(...) }
func TestB(t *testing.T) { sharedDB.Query(...) }
```

✅ **Do: Fresh state per test** (already implemented)

### ❌ Don't: Sleep in Tests

```go
// Bad - slow, flaky, arbitrary
time.Sleep(5 * time.Second)
```

✅ **Do: Use synchronization or mocks**

### ❌ Don't: Ignore Race Detector Warnings

```bash
# Bad
go test ./...  # race conditions not detected
```

✅ **Do: Always use -race** (already in CI)

### ❌ Don't: Skip CI on "Small Changes"

```yaml
# Bad
on:
  push:
    paths-ignore:
      - '**'  # Skip everything
```

✅ **Do: Always run CI** - "small" bugs are still bugs

## Recommended Additions

### 1. **Pre-commit Hooks**

Create `.git/hooks/pre-commit`:
```bash
#!/bin/bash
go fmt ./...
go vet ./...
go test ./...
```

Or use https://pre-commit.com/

### 2. **Local CI Simulation**

```bash
# Runs same checks as CI
make ci-local
```

Create `Makefile`:
```makefile
.PHONY: ci-local
ci-local:
	go mod verify
	go fmt ./...
	go vet ./...
	staticcheck ./...
	go test -race -cover ./...
```

### 3. **Coverage Requirements**

```yaml
- name: Check coverage threshold
  run: |
    total=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}' | sed 's/%//')
    if (( $(echo "$total < 50.0" | bc -l) )); then
      echo "Coverage $total% is below 50%"
      exit 1
    fi
```

**Recommended thresholds:**
- Critical packages (db, onboarding): 60-80%
- Utility packages: 40-60%
- Generated code: Can exclude

### 4. **Automated Dependency Updates**

Use Dependabot (`.github/dependabot.yml`):
```yaml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
```

## Summary

### ✅ What We're Doing Right

1. **No external dependencies** - All tests use mocks
2. **Fast execution** - Tests complete in milliseconds
3. **Race detection** - Catches concurrency bugs
4. **Parallel execution** - Faster feedback
5. **Code quality checks** - Fmt, vet, staticcheck, lint
6. **Coverage tracking** - Codecov integration
7. **Proper caching** - Go modules and build cache

### 🎯 Quick Wins to Add

1. Local CI simulation (`make ci-local`)
2. Pre-commit hooks
3. Dependabot for dependency updates
4. Coverage threshold enforcement (if desired)

### 📊 Success Metrics

Track these over time:
- Test execution time (target: <5min total)
- Coverage percentage (target: 60%+)
- Test count (growing with features)
- CI success rate (target: >95%)
- Time to feedback (<5min from push to result)

## Getting Help

- **Flaky tests?** Check for shared state, timeouts, or race conditions
- **Slow CI?** Profile tests, check caching, consider parallelization
- **Coverage dropping?** Add tests for new code paths
- **CI failing locally passing?** Check Go version, environment differences

## References

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [GitHub Actions Go Guide](https://docs.github.com/en/actions/guides/building-and-testing-go)
- [Table-Driven Tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [Advanced Testing in Go](https://about.sourcegraph.com/blog/go/advanced-testing-in-go)
