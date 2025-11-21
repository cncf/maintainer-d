# Onboarding Service Testing

## Overview

This directory contains test infrastructure for the onboarding service, enabling comprehensive testing of the `fossaChosen` function and webhook handlers without requiring external API access.

## Test Infrastructure Files

### Core Test Files

- **`server_test.go`** - Unit tests for `fossaChosen` and other server functions
- **`github_mock.go`** - Mock GitHub HTTP transport that captures API calls
- **`fossa_mock.go`** - Mock FOSSA client that simulates API behavior
- **`test_helpers.go`** - Helper functions for database setup and test data
- **`interfaces.go`** - Interface definition for testable FOSSA client

### Documentation

- **`TESTING_STRATEGY.md`** - Comprehensive testing strategy and patterns
- **`TESTING_QUICKSTART.md`** - Quick reference guide

## Running Tests

```bash
# Run all tests
go test -v

# Run specific test
go test -v -run TestFossaChosen_Basic

# Run with coverage
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Architecture

### 1. In-Memory Database
Uses SQLite `:memory:` database for fast, isolated tests:
```go
db := setupTestDB(t)
project, maintainers := seedProjectData(t, db)
```

### 2. Mock GitHub Client
Captures GitHub API calls without hitting real API:
```go
mockGitHub := NewMockGitHubTransport()
httpClient := &http.Client{Transport: mockGitHub}
ghClient := github.NewClient(httpClient)

// Later verify
comments := mockGitHub.GetCreatedComments()
assert.Contains(t, comments[0].Body, "expected text")
```

### 3. Mock FOSSA Client
Simulates FOSSA API behavior in memory:
```go
mockFossa := NewMockFossaClient()
mockFossa.SetUserExists("alice@example.com", true)

// Later verify
invites := mockFossa.GetInvitationsSent()
teams := mockFossa.GetTeamsCreated()
```

## Key Features

✅ **Fast** - Tests run in ~16ms  
✅ **Isolated** - No external dependencies  
✅ **CI-Friendly** - No API tokens needed  
✅ **Comprehensive** - Verifies GitHub comment content, FOSSA API calls, database state  
✅ **Maintainable** - Clear helper functions and test structure  

## Example Test

```go
func TestFossaChosen_Success(t *testing.T) {
    // Setup in-memory database
    db := setupTestDB(t)
    project, _ := seedProjectData(t, db)
    
    // Create mocks
    mockFossa := NewMockFossaClient()
    mockGitHub := NewMockGitHubTransport()
    
    // Create server
    server := createTestServer(t, db, mockFossa, mockGitHub)
    
    // Execute
    issueEvent := createIssueLabeledEvent(project.Name, "fossa", 42)
    req, _ := http.NewRequest("POST", "/webhook", nil)
    server.fossaChosen(project.Name, req, issueEvent)
    
    // Verify
    assert.Len(t, mockFossa.GetTeamsCreated(), 1)
    assert.Len(t, mockFossa.GetInvitationsSent(), 2)
    
    comments := mockGitHub.GetCreatedComments()
    assert.Contains(t, comments[0].Body, "has been created in FOSSA")
}
```

## Current Test Coverage

### TestMockInfrastructure
- ✅ Mock GitHub transport captures comments
- ✅ Mock FOSSA client tracks invitations
- ✅ Mock FOSSA client creates teams
- ✅ In-memory database works
- ✅ Helper functions create proper events

### TestFossaChosen_Basic
- ✅ Successful onboarding with new team
- ✅ Maintainer already has pending invitation
- ✅ Maintainer already exists in FOSSA

## Adding More Tests

Follow the pattern in `server_test.go`:

1. Setup database and seed data
2. Create and configure mocks
3. Execute the function under test
4. Verify interactions with mocks
5. Verify database state if needed

See `TESTING_STRATEGY.md` for more test scenarios to implement.

## Architecture Changes

### Interface Extraction
The `EventListener.FossaClient` field was changed from `*fossa.Client` to `FossaClientInterface` to enable dependency injection and testing.

Production code creates real clients:
```go
server.FossaClient = fossa.NewClient(token)
```

Test code uses mocks:
```go
server.FossaClient = NewMockFossaClient()
```

## Benefits

1. **No External Dependencies** - Tests don't require GitHub or FOSSA API tokens
2. **Fast Execution** - In-memory database and mocks make tests complete in milliseconds
3. **Deterministic** - Mocks ensure consistent, reproducible results
4. **Easy Debugging** - Captured requests/responses available for inspection
5. **CI/CD Ready** - No special configuration or secrets needed

## Next Steps

See `TESTING_STRATEGY.md` for:
- Additional test scenarios for webhook handlers
- Integration tests for full workflows
- Authorization testing patterns
- Error case coverage
