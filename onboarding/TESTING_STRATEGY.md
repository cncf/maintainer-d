# Testing Strategy for onboarding/server.go

## Overview

This document outlines a comprehensive testing strategy for the onboarding server code, with special focus on the `fossaChosen` function. The strategy leverages in-memory SQLite databases (similar to `db/store_impl_test.go`) and mock HTTP clients to test GitHub interactions in isolation.

## Testing Architecture

### 1. **Unit Tests** (`server_test.go`)
Test individual functions in isolation with mocked dependencies.

### 2. **Integration Tests** (`server_integration_test.go`)
Test multiple components working together with real database interactions but mocked external services.

### 3. **End-to-End Tests** (`server_e2e_test.go`)
Optional tests against real services (skipped in CI unless flags are set).

---

## Key Testing Challenges & Solutions

### Challenge 1: External Dependencies
**Problem**: The code depends on:
- FOSSA API (`FossaClient`)
- GitHub API (`GitHubClient`)
- SQLite Database (`Store`)

**Solution**: Use the following patterns:
1. **In-memory SQLite**: Just like `db/store_impl_test.go`, use `:memory:` databases
2. **Mock HTTP Transport**: Create mock HTTP roundtrippers for GitHub/FOSSA
3. **Interface Extraction**: Extract interfaces for testability

### Challenge 2: Testing GitHub Issue Updates
**Problem**: `fossaChosen` and `handleWebhook` post comments to GitHub issues. We need to verify:
- Comment content is correct
- Comments are posted to the right issue
- Error handling works properly

**Solution**: 
- Mock the GitHub client's HTTP transport
- Capture and verify HTTP requests
- Use recorded responses for deterministic testing

### Challenge 3: State Management
**Problem**: Testing involves complex state across multiple systems (DB, FOSSA, GitHub).

**Solution**:
- Use table-driven tests with well-defined initial states
- Create helper functions for common test scenarios
- Use fixtures for consistent test data

---

## Detailed Testing Strategy

### Phase 1: Test Infrastructure Setup

#### 1.1 Test Fixtures and Helpers

```go
// server_test.go

package onboarding

import (
    "context"
    "maintainerd/db"
    "maintainerd/model"
    "testing"
    
    "github.com/google/go-github/v55/github"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// setupTestDB creates an in-memory database with schema
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Silent),
    })
    require.NoError(t, err)
    
    err = db.AutoMigrate(
        &model.Company{},
        &model.Project{},
        &model.Maintainer{},
        &model.MaintainerProject{},
        &model.Service{},
        &model.ServiceTeam{},
    )
    require.NoError(t, err)
    
    return db
}

// seedProjectData creates a project with maintainers
func seedProjectData(t *testing.T, db *gorm.DB) (model.Project, []model.Maintainer) {
    company := model.Company{Name: "Test Company"}
    require.NoError(t, db.Create(&company).Error)
    
    project := model.Project{Name: "test-project", Maturity: model.Graduated}
    require.NoError(t, db.Create(&project).Error)
    
    maintainers := []model.Maintainer{
        {
            Name:             "Alice Developer",
            Email:            "alice@example.com",
            GitHubAccount:    "alice",
            MaintainerStatus: model.ActiveMaintainer,
            CompanyID:        &company.ID,
        },
        {
            Name:             "Bob Engineer",
            Email:            "bob@example.com",
            GitHubAccount:    "bob",
            MaintainerStatus: model.ActiveMaintainer,
            CompanyID:        &company.ID,
        },
    }
    
    for i := range maintainers {
        require.NoError(t, db.Create(&maintainers[i]).Error)
    }
    
    // Associate maintainers with project
    require.NoError(t, db.Model(&project).Association("Maintainers").Append(maintainers))
    
    return project, maintainers
}
```

#### 1.2 Mock GitHub Client

```go
// github_mock.go

package onboarding

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "sync"
    
    "github.com/google/go-github/v55/github"
)

// MockGitHubTransport captures GitHub API calls
type MockGitHubTransport struct {
    mu              sync.Mutex
    requests        []*http.Request
    responses       map[string]*http.Response
    createdComments []GitHubCommentCapture
}

type GitHubCommentCapture struct {
    Owner       string
    Repo        string
    IssueNumber int
    Body        string
}

func NewMockGitHubTransport() *MockGitHubTransport {
    return &MockGitHubTransport{
        responses: make(map[string]*http.Response),
    }
}

func (m *MockGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.requests = append(m.requests, req)
    
    // Capture comment creation
    if req.Method == "POST" && strings.Contains(req.URL.Path, "/issues/") && strings.HasSuffix(req.URL.Path, "/comments") {
        body, _ := io.ReadAll(req.Body)
        req.Body = io.NopCloser(bytes.NewReader(body))
        
        var comment github.IssueComment
        json.Unmarshal(body, &comment)
        
        // Parse owner, repo, issue number from URL
        parts := strings.Split(req.URL.Path, "/")
        capture := GitHubCommentCapture{
            Owner:       parts[2],
            Repo:        parts[3],
            IssueNumber: parseIssueNumber(parts[5]),
            Body:        comment.GetBody(),
        }
        m.createdComments = append(m.createdComments, capture)
        
        // Return success response
        resp := &http.Response{
            StatusCode: 201,
            Body:       io.NopCloser(bytes.NewReader([]byte(`{"id": 1}`))),
            Header:     make(http.Header),
        }
        return resp, nil
    }
    
    // Return configured response or default 200
    key := req.Method + " " + req.URL.Path
    if resp, ok := m.responses[key]; ok {
        return resp, nil
    }
    
    return &http.Response{
        StatusCode: 200,
        Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
        Header:     make(http.Header),
    }, nil
}

func (m *MockGitHubTransport) GetCreatedComments() []GitHubCommentCapture {
    m.mu.Lock()
    defer m.mu.Unlock()
    return append([]GitHubCommentCapture{}, m.createdComments...)
}

func (m *MockGitHubTransport) Reset() {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.requests = nil
    m.createdComments = nil
}
```

#### 1.3 Mock FOSSA Client

```go
// fossa_mock.go

package onboarding

import (
    "errors"
    "maintainerd/plugins/fossa"
    "sync"
)

// MockFossaClient simulates FOSSA API behavior
type MockFossaClient struct {
    mu                sync.Mutex
    teams             map[string]*fossa.Team
    invitations       map[string]bool // email -> pending
    teamMembers       map[int][]string // teamID -> emails
    userExists        map[string]bool // email -> exists
    nextTeamID        int
    
    // Capture calls for verification
    invitationsSent   []string
    teamsCreated      []string
    membersAdded      map[int][]string // teamID -> emails added
}

func NewMockFossaClient() *MockFossaClient {
    return &MockFossaClient{
        teams:         make(map[string]*fossa.Team),
        invitations:   make(map[string]bool),
        teamMembers:   make(map[int][]string),
        userExists:    make(map[string]bool),
        membersAdded:  make(map[int][]string),
        nextTeamID:    1000,
    }
}

func (m *MockFossaClient) CreateTeam(name string) (*fossa.Team, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if _, exists := m.teams[name]; exists {
        return nil, errors.New("team already exists")
    }
    
    team := &fossa.Team{
        ID:   m.nextTeamID,
        Name: name,
    }
    m.nextTeamID++
    m.teams[name] = team
    m.teamsCreated = append(m.teamsCreated, name)
    m.teamMembers[team.ID] = []string{}
    
    return team, nil
}

func (m *MockFossaClient) SendUserInvitation(email string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.invitationsSent = append(m.invitationsSent, email)
    
    if m.userExists[email] {
        return fossa.ErrUserAlreadyMember
    }
    
    if m.invitations[email] {
        return fossa.ErrInviteAlreadyExists
    }
    
    m.invitations[email] = true
    return nil
}

func (m *MockFossaClient) HasPendingInvitation(email string) (bool, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.invitations[email], nil
}

func (m *MockFossaClient) FetchTeamUserEmails(teamID int) ([]string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    emails, ok := m.teamMembers[teamID]
    if !ok {
        return nil, errors.New("team not found")
    }
    return append([]string{}, emails...), nil
}

func (m *MockFossaClient) AddUserToTeamByEmail(teamID int, email string, roleID int) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if !m.userExists[email] {
        return errors.New("user not found")
    }
    
    members := m.teamMembers[teamID]
    for _, e := range members {
        if e == email {
            return fossa.ErrUserAlreadyMember
        }
    }
    
    m.teamMembers[teamID] = append(members, email)
    m.membersAdded[teamID] = append(m.membersAdded[teamID], email)
    return nil
}

func (m *MockFossaClient) FetchImportedRepos(teamID int) (int, []fossa.Repo, error) {
    // Return empty by default
    return 0, []fossa.Repo{}, nil
}

func (m *MockFossaClient) ImportedProjectLinks(repos []fossa.Repo) string {
    return ""
}

// Test helpers
func (m *MockFossaClient) SetUserExists(email string, exists bool) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.userExists[email] = exists
}

func (m *MockFossaClient) AcceptInvitation(email string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    delete(m.invitations, email)
    m.userExists[email] = true
}
```

---

### Phase 2: Testing `fossaChosen` Function

The `fossaChosen` function is critical because it:
1. Retrieves project data from the database
2. Creates FOSSA team (if needed)
3. Sends invitations to maintainers
4. Posts a formatted comment to GitHub issue

#### Test Cases for `fossaChosen`

```go
// server_test.go

func TestFossaChosen(t *testing.T) {
    tests := []struct {
        name                  string
        setupDB               func(*gorm.DB) (model.Project, []model.Maintainer)
        setupFossa            func(*MockFossaClient)
        projectName           string
        expectedCommentRegex  []string // Patterns that should appear in comment
        expectedTeamsCreated  int
        expectedInvitesSent   int
        expectError           bool
    }{
        {
            name: "successful onboarding - new team",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                return seedProjectData(t, db)
            },
            setupFossa: func(fc *MockFossaClient) {
                // No pre-existing state
            },
            projectName: "test-project",
            expectedCommentRegex: []string{
                `maintainer-d CNCF FOSSA onboarding - Report`,
                `test-project has 2 maintainers registered`,
                `test-project team.*has been created in FOSSA`,
                `@alice`,
                `@bob`,
                `/fossa-invite accepted`,
            },
            expectedTeamsCreated: 1,
            expectedInvitesSent:  2,
            expectError:          false,
        },
        {
            name: "team already exists - send invites only",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                project, maintainers := seedProjectData(t, db)
                
                // Create service team record
                service := model.Service{Name: "FOSSA"}
                db.Create(&service)
                
                st := model.ServiceTeam{
                    ServiceTeamID:   1001,
                    ServiceID:       service.ID,
                    ServiceTeamName: stringPtr("test-project"),
                    ProjectID:       project.ID,
                    ProjectName:     stringPtr("test-project"),
                }
                db.Create(&st)
                
                return project, maintainers
            },
            setupFossa: func(fc *MockFossaClient) {
                fc.CreateTeam("test-project") // Pre-create team
            },
            projectName: "test-project",
            expectedCommentRegex: []string{
                `test-project team.*was already in FOSSA`,
            },
            expectedTeamsCreated: 1, // Pre-created
            expectedInvitesSent:  2,
            expectError:          false,
        },
        {
            name: "maintainer already has pending invitation",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                return seedProjectData(t, db)
            },
            setupFossa: func(fc *MockFossaClient) {
                // Simulate pending invitation
                fc.invitations["alice@example.com"] = true
            },
            projectName: "test-project",
            expectedCommentRegex: []string{
                `@alice.*pending invitation`,
                `@bob`, // Bob should still get invited
            },
            expectedTeamsCreated: 1,
            expectedInvitesSent:  2, // Both called, one returns error
            expectError:          false,
        },
        {
            name: "maintainer already FOSSA member",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                return seedProjectData(t, db)
            },
            setupFossa: func(fc *MockFossaClient) {
                fc.SetUserExists("alice@example.com", true)
            },
            projectName: "test-project",
            expectedCommentRegex: []string{
                `@alice.*CNCF FOSSA User`,
            },
            expectedTeamsCreated: 1,
            expectedInvitesSent:  2,
            expectError:          false,
        },
        {
            name: "no maintainers registered",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                project := model.Project{Name: "empty-project", Maturity: model.Sandbox}
                db.Create(&project)
                return project, []model.Maintainer{}
            },
            setupFossa:            func(fc *MockFossaClient) {},
            projectName:           "empty-project",
            expectedCommentRegex: []string{
                `Maintainers not yet registered`,
                `❌.*encountered some problems`,
            },
            expectedTeamsCreated: 1,
            expectedInvitesSent:  0,
            expectError:          true,
        },
        {
            name: "project not in database",
            setupDB: func(db *gorm.DB) (model.Project, []model.Maintainer) {
                return model.Project{}, []model.Maintainer{}
            },
            setupFossa:            func(fc *MockFossaClient) {},
            projectName:           "non-existent",
            expectedCommentRegex: []string{},
            expectedTeamsCreated: 0,
            expectedInvitesSent:  0,
            expectError:          true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup
            db := setupTestDB(t)
            project, _ := tt.setupDB(db)
            
            mockFossa := NewMockFossaClient()
            tt.setupFossa(mockFossa)
            
            mockGitHub := NewMockGitHubTransport()
            httpClient := &http.Client{Transport: mockGitHub}
            ghClient := github.NewClient(httpClient)
            
            store := db.NewSQLStore(db)
            
            server := &EventListener{
                Store:        store,
                FossaClient:  mockFossa,
                GitHubClient: ghClient,
                Projects:     map[string]model.Project{project.Name: project},
            }
            
            // Create fake issue event
            issueEvent := &github.IssuesEvent{
                Action: github.String("labeled"),
                Issue: &github.Issue{
                    Number: github.Int(42),
                    Title:  github.String("[PROJECT ONBOARDING] " + tt.projectName),
                },
                Repo: &github.Repository{
                    Owner: &github.User{Login: github.String("cncf")},
                    Name:  github.String("onboarding"),
                },
            }
            
            req, _ := http.NewRequest("POST", "/webhook", nil)
            
            // Execute
            server.fossaChosen(tt.projectName, req, issueEvent)
            
            // Verify GitHub comment
            comments := mockGitHub.GetCreatedComments()
            
            if !tt.expectError || len(tt.expectedCommentRegex) > 0 {
                require.Len(t, comments, 1, "Should create exactly one GitHub comment")
                comment := comments[0]
                
                assert.Equal(t, "cncf", comment.Owner)
                assert.Equal(t, "onboarding", comment.Repo)
                assert.Equal(t, 42, comment.IssueNumber)
                
                // Verify comment content
                for _, pattern := range tt.expectedCommentRegex {
                    assert.Regexp(t, pattern, comment.Body, 
                        "Comment should contain pattern: %s", pattern)
                }
            }
            
            // Verify FOSSA interactions
            assert.Len(t, mockFossa.teamsCreated, tt.expectedTeamsCreated,
                "Should create expected number of teams")
            assert.Len(t, mockFossa.invitationsSent, tt.expectedInvitesSent,
                "Should send expected number of invitations")
        })
    }
}
```

---

### Phase 3: Testing Webhook Handler

The webhook handler is more complex as it handles multiple event types and authorization.

```go
func TestHandleWebhook_IssueCommentEvent(t *testing.T) {
    tests := []struct {
        name                string
        setupDB             func(*gorm.DB) model.Project
        setupFossa          func(*MockFossaClient)
        commentBody         string
        commentAuthor       string
        issueTitle          string
        issueAssignees      []string
        expectedComment     []string // Patterns in response comment
        expectCommentPosted bool
    }{
        {
            name: "authorized maintainer accepts invite",
            setupDB: func(db *gorm.DB) model.Project {
                project, _ := seedProjectData(t, db)
                // Add FOSSA service team
                service := model.Service{Name: "FOSSA"}
                db.Create(&service)
                st := model.ServiceTeam{
                    ServiceTeamID:   1001,
                    ServiceID:       service.ID,
                    ProjectID:       project.ID,
                }
                db.Create(&st)
                return project
            },
            setupFossa: func(fc *MockFossaClient) {
                fc.SetUserExists("alice@example.com", true)
            },
            commentBody:         "/fossa-invite accepted",
            commentAuthor:       "alice",
            issueTitle:          "[PROJECT ONBOARDING] test-project",
            issueAssignees:      []string{},
            expectedComment:     []string{`@alice.*already a member`},
            expectCommentPosted: true,
        },
        {
            name: "unauthorized user tries to accept",
            setupDB: func(db *gorm.DB) model.Project {
                project, _ := seedProjectData(t, db)
                return project
            },
            setupFossa:          func(fc *MockFossaClient) {},
            commentBody:         "/fossa-invite accepted",
            commentAuthor:       "hacker",
            issueTitle:          "[PROJECT ONBOARDING] test-project",
            issueAssignees:      []string{},
            expectedComment:     []string{`not authorized`},
            expectCommentPosted: true,
        },
        {
            name: "assignee accepts invite",
            setupDB: func(db *gorm.DB) model.Project {
                project, _ := seedProjectData(t, db)
                service := model.Service{Name: "FOSSA"}
                db.Create(&service)
                st := model.ServiceTeam{
                    ServiceTeamID: 1001,
                    ServiceID:     service.ID,
                    ProjectID:     project.ID,
                }
                db.Create(&st)
                return project
            },
            setupFossa: func(fc *MockFossaClient) {
                fc.SetUserExists("charlie@example.com", true)
            },
            commentBody:         "/fossa-invite accepted",
            commentAuthor:       "charlie",
            issueTitle:          "[PROJECT ONBOARDING] test-project",
            issueAssignees:      []string{"charlie"},
            expectedComment:     []string{`FOSSA Team Membership Update`},
            expectCommentPosted: true,
        },
        {
            name: "team not found",
            setupDB: func(db *gorm.DB) model.Project {
                project, _ := seedProjectData(t, db)
                // Don't create ServiceTeam
                return project
            },
            setupFossa:          func(fc *MockFossaClient) {},
            commentBody:         "/fossa-invite accepted",
            commentAuthor:       "alice",
            issueTitle:          "[PROJECT ONBOARDING] test-project",
            issueAssignees:      []string{},
            expectedComment:     []string{`team.*was not found`, `add the 'fossa' label`},
            expectCommentPosted: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation similar to above
            // ... (setup, execute, verify)
        })
    }
}
```

---

### Phase 4: Integration Tests

Integration tests verify the full flow with real database but mocked external services.

```go
// server_integration_test.go

func TestFullOnboardingFlow(t *testing.T) {
    // Setup
    db := setupTestDB(t)
    project, maintainers := seedProjectData(t, db)
    
    mockFossa := NewMockFossaClient()
    mockGitHub := NewMockGitHubTransport()
    
    server := setupServer(t, db, mockFossa, mockGitHub)
    
    // Step 1: Label issue with "fossa"
    labelEvent := createIssueLabeledEvent("test-project", "fossa", 42)
    handleEvent(t, server, labelEvent)
    
    // Verify team created and invites sent
    assert.Len(t, mockFossa.teamsCreated, 1)
    assert.Len(t, mockFossa.invitationsSent, 2)
    
    // Verify GitHub comment posted
    comments := mockGitHub.GetCreatedComments()
    require.Len(t, comments, 1)
    assert.Contains(t, comments[0].Body, "/fossa-invite accepted")
    
    // Step 2: Maintainer accepts invitation (simulate in FOSSA)
    mockFossa.AcceptInvitation("alice@example.com")
    mockGitHub.Reset()
    
    // Step 3: Post acceptance comment
    commentEvent := createIssueCommentEvent("test-project", "/fossa-invite accepted", "alice", 42)
    handleEvent(t, server, commentEvent)
    
    // Verify maintainer added to team
    members := mockFossa.membersAdded[1000]
    assert.Contains(t, members, "alice@example.com")
    
    // Verify confirmation comment
    comments = mockGitHub.GetCreatedComments()
    require.Len(t, comments, 1)
    assert.Regexp(t, `@alice.*added to FOSSA team`, comments[0].Body)
}
```

---

## CI/CD Integration

### GitHub Actions Workflow

```yaml
# .github/workflows/test-onboarding.yml
name: Test Onboarding Service

on:
  pull_request:
    paths:
      - 'onboarding/**'
      - 'db/**'
      - 'model/**'
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run unit tests
        run: |
          cd onboarding
          go test -v -race -coverprofile=coverage.out -covermode=atomic
      
      - name: Run integration tests
        run: |
          cd onboarding
          go test -v -tags=integration ./...
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./onboarding/coverage.out
          flags: onboarding
```

---

## Testing Utilities

### Helper Functions

```go
// test_helpers.go

// stringPtr returns a pointer to string
func stringPtr(s string) *string {
    return &s
}

// intPtr returns a pointer to int
func intPtr(i int) *int {
    return &i
}

// createIssueLabeledEvent creates a fake GitHub issue labeled event
func createIssueLabeledEvent(projectName, label string, issueNum int) *github.IssuesEvent {
    return &github.IssuesEvent{
        Action: github.String("labeled"),
        Label:  &github.Label{Name: github.String(label)},
        Issue: &github.Issue{
            Number: github.Int(issueNum),
            Title:  github.String("[PROJECT ONBOARDING] " + projectName),
        },
        Repo: &github.Repository{
            Owner: &github.User{Login: github.String("cncf")},
            Name:  github.String("onboarding"),
        },
    }
}

// createIssueCommentEvent creates a fake comment event
func createIssueCommentEvent(projectName, body, author string, issueNum int) *github.IssueCommentEvent {
    return &github.IssueCommentEvent{
        Action: github.String("created"),
        Comment: &github.IssueComment{
            Body: github.String(body),
            User: &github.User{Login: github.String(author)},
        },
        Issue: &github.Issue{
            Number: github.Int(issueNum),
            Title:  github.String("[PROJECT ONBOARDING] " + projectName),
        },
        Repo: &github.Repository{
            Owner: &github.User{Login: github.String("cncf")},
            Name:  github.String("onboarding"),
        },
    }
}
```

---

## Benefits of This Approach

1. **Fast**: In-memory database and mocked HTTP means tests run in milliseconds
2. **Deterministic**: No external dependencies means consistent results
3. **Comprehensive**: Tests cover success paths, error cases, and edge cases
4. **CI-friendly**: No API tokens or external services needed
5. **Maintainable**: Clear test structure with reusable helpers
6. **Debuggable**: Captured HTTP requests/responses for troubleshooting

---

## Next Steps

1. Implement `server_test.go` with basic unit tests
2. Add GitHub/FOSSA mocks
3. Implement `fossaChosen` test cases
4. Add integration tests for full flows
5. Add test coverage reporting
6. Document how to run tests locally and in CI

---

## Summary

This strategy provides:
- ✅ Isolated unit tests for individual functions
- ✅ Integration tests for full workflows
- ✅ Mocked external services (GitHub, FOSSA)
- ✅ In-memory database (fast, no cleanup needed)
- ✅ Verification of GitHub issue comments
- ✅ CI/CD integration
- ✅ High code coverage potential

The key insight is that by mocking the HTTP transport layer, we can verify that the correct GitHub API calls are made with the correct payloads, without actually calling GitHub's API.
