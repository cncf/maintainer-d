# Testing Quick Start Guide

## TL;DR

We can test `fossaChosen` and other onboarding functions using:

1. **In-memory SQLite** (like `db/store_impl_test.go`) - fast, no cleanup
2. **Mock HTTP clients** - capture GitHub API calls without hitting real API
3. **Mock FOSSA client** - simulate FOSSA behavior in memory

## Key Testing Patterns

### Pattern 1: In-Memory Database (Already Working!)

```go
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Silent),
    })
    require.NoError(t, err)
    db.AutoMigrate(&model.Project{}, &model.Maintainer{}, ...)
    return db
}
```

**Benefits**: Fast, isolated, no cleanup needed, works in CI

### Pattern 2: Mock GitHub to Verify Issue Comments

```go
type MockGitHubTransport struct {
    createdComments []GitHubCommentCapture
}

func (m *MockGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Capture comment creation requests
    if req.Method == "POST" && strings.Contains(req.URL.Path, "/comments") {
        // Parse and store comment details
        m.createdComments = append(m.createdComments, capture)
    }
    return mockResponse, nil
}

// In test:
httpClient := &http.Client{Transport: mockTransport}
ghClient := github.NewClient(httpClient)

// After fossaChosen runs:
comments := mockTransport.GetCreatedComments()
assert.Contains(t, comments[0].Body, "expected text")
```

**Benefits**: Verify exact comment content, no GitHub API calls, works offline

### Pattern 3: Mock FOSSA Client

```go
type MockFossaClient struct {
    invitationsSent []string
    teamsCreated    []string
    userExists      map[string]bool
}

func (m *MockFossaClient) SendUserInvitation(email string) error {
    m.invitationsSent = append(m.invitationsSent, email)
    if m.userExists[email] {
        return fossa.ErrUserAlreadyMember
    }
    return nil
}

// In test, verify:
assert.Len(t, mockFossa.invitationsSent, 2)
assert.Contains(t, mockFossa.invitationsSent, "alice@example.com")
```

**Benefits**: Fast, predictable, can simulate errors, works in CI

## Example Test for `fossaChosen`

```go
func TestFossaChosen_Success(t *testing.T) {
    // 1. Setup in-memory database
    db := setupTestDB(t)
    project, maintainers := seedProjectData(t, db)
    
    // 2. Create mocks
    mockFossa := NewMockFossaClient()
    mockGitHub := NewMockGitHubTransport()
    ghClient := github.NewClient(&http.Client{Transport: mockGitHub})
    
    // 3. Create server
    server := &EventListener{
        Store:        db.NewSQLStore(db),
        FossaClient:  mockFossa,
        GitHubClient: ghClient,
        Projects:     map[string]model.Project{project.Name: project},
    }
    
    // 4. Create fake event
    issueEvent := &github.IssuesEvent{
        Issue: &github.Issue{
            Number: github.Int(42),
            Title:  github.String("[PROJECT ONBOARDING] test-project"),
        },
        Repo: &github.Repository{
            Owner: &github.User{Login: github.String("cncf")},
            Name:  github.String("onboarding"),
        },
    }
    
    req, _ := http.NewRequest("POST", "/webhook", nil)
    
    // 5. Execute
    server.fossaChosen("test-project", req, issueEvent)
    
    // 6. Verify
    // Check FOSSA calls
    assert.Len(t, mockFossa.teamsCreated, 1)
    assert.Len(t, mockFossa.invitationsSent, 2)
    assert.Contains(t, mockFossa.invitationsSent, "alice@example.com")
    
    // Check GitHub comment
    comments := mockGitHub.GetCreatedComments()
    require.Len(t, comments, 1)
    
    comment := comments[0]
    assert.Equal(t, "cncf", comment.Owner)
    assert.Equal(t, "onboarding", comment.Repo)
    assert.Equal(t, 42, comment.IssueNumber)
    
    // Verify comment content
    assert.Contains(t, comment.Body, "maintainer-d CNCF FOSSA onboarding")
    assert.Contains(t, comment.Body, "test-project has 2 maintainers")
    assert.Contains(t, comment.Body, "team.*has been created")
    assert.Contains(t, comment.Body, "@alice")
    assert.Contains(t, comment.Body, "@bob")
    assert.Contains(t, comment.Body, "/fossa-invite accepted")
}
```

## Test Scenarios to Cover

### For `fossaChosen`:

1. ✅ **New project onboarding** - team created, invites sent, comment posted
2. ✅ **Team already exists** - skip team creation, send invites
3. ✅ **Maintainer has pending invite** - comment mentions pending
4. ✅ **Maintainer already member** - comment mentions already member
5. ✅ **No maintainers** - error handling, error in comment
6. ✅ **Project not found** - graceful error handling
7. ✅ **FOSSA team creation fails** - error in comment
8. ✅ **Some repos already imported** - mention in comment

### For webhook handler:

1. ✅ **Authorized maintainer** - action succeeds
2. ✅ **Unauthorized user** - "not authorized" comment
3. ✅ **Issue assignee** - action succeeds
4. ✅ **Wrong comment body** - ignored
5. ✅ **Service team not found** - helpful error message
6. ✅ **Invalid issue title** - gracefully handled

## CI/CD Integration

### What You Need:

```yaml
# .github/workflows/test.yml
- name: Run tests
  run: go test -v -race ./onboarding/...
```

### What You DON'T Need:

- ❌ FOSSA_API_TOKEN
- ❌ GITHUB_TOKEN  
- ❌ Real database
- ❌ Internet connection
- ❌ Test fixtures files
- ❌ Docker containers

## File Structure

```
onboarding/
├── server.go                    # Production code
├── server_test.go              # Unit tests (NEW)
├── server_integration_test.go  # Integration tests (NEW)
├── github_mock.go              # GitHub mock (NEW)
├── fossa_mock.go               # FOSSA mock (NEW)
├── test_helpers.go             # Test utilities (NEW)
└── TESTING_STRATEGY.md         # Full documentation
```

## Implementation Order

1. **Phase 1**: Create mock infrastructure
   - `github_mock.go` - HTTP transport that captures requests
   - `fossa_mock.go` - In-memory FOSSA simulator
   - `test_helpers.go` - Database setup, seed data

2. **Phase 2**: Write unit tests for `fossaChosen`
   - Start with happy path
   - Add error cases
   - Add edge cases

3. **Phase 3**: Write webhook handler tests
   - Test authorization logic
   - Test event routing
   - Test comment handling

4. **Phase 4**: Integration tests
   - Full flow: label → invite → accept → added

## Why This Approach Works

| Aspect | Traditional | Our Approach |
|--------|-------------|--------------|
| **Speed** | Slow (API calls) | Fast (in-memory) |
| **Reliability** | Flaky (network) | Stable (no I/O) |
| **Setup** | Complex (tokens, accounts) | Simple (just code) |
| **CI** | Difficult (secrets) | Easy (no config) |
| **Debugging** | Hard (logs) | Easy (captured data) |
| **Coverage** | Limited (API limits) | Complete (all paths) |

## Questions Answered

**Q: How do we test GitHub issue updates without calling GitHub?**  
A: Mock the HTTP transport layer to capture requests. Verify the comment body.

**Q: How do we test database interactions?**  
A: Use in-memory SQLite, just like `db/store_impl_test.go`.

**Q: How do we test FOSSA API calls?**  
A: Create a mock client that simulates FOSSA behavior in memory.

**Q: Can this run in CI without secrets?**  
A: Yes! All mocks work without any external dependencies or tokens.

**Q: How do we verify the comment content?**  
A: The mock captures the exact text posted. Use assertions/regex to verify.

**Q: What about testing order of operations?**  
A: Mocks can track call order and state changes over time.

## Next Steps

See `TESTING_STRATEGY.md` for:
- Complete code examples
- Full test case list
- Mock implementations
- Integration test patterns
- CI/CD configuration

Start with implementing the mocks, then write tests one by one!
