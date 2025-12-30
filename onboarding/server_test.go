package onboarding

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-github/v55/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maintainerd/model"
)

func TestMockInfrastructure(t *testing.T) {
	t.Run("mock GitHub transport captures comments", func(t *testing.T) {
		mockGitHub := NewMockGitHubTransport()
		httpClient := &http.Client{Transport: mockGitHub}
		ghClient := github.NewClient(httpClient)

		// Post a comment
		comment := &github.IssueComment{
			Body: github.String("Test comment"),
		}
		req, _ := http.NewRequest("GET", "/", nil)
		_, _, err := ghClient.Issues.CreateComment(req.Context(), "test-org", "test-repo", 42, comment)
		require.NoError(t, err)

		// Verify it was captured
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Equal(t, "test-org", comments[0].Owner)
		assert.Equal(t, "test-repo", comments[0].Repo)
		assert.Equal(t, 42, comments[0].IssueNumber)
		assert.Equal(t, "Test comment", comments[0].Body)
	})

	t.Run("mock FOSSA client tracks invitations", func(t *testing.T) {
		mockFossa := NewMockFossaClient()

		// Send invitations
		err := mockFossa.SendUserInvitation("alice@example.com")
		require.NoError(t, err)

		err = mockFossa.SendUserInvitation("bob@example.com")
		require.NoError(t, err)

		// Verify
		sent := mockFossa.GetInvitationsSent()
		require.Len(t, sent, 2)
		assert.Contains(t, sent, "alice@example.com")
		assert.Contains(t, sent, "bob@example.com")

		// Check pending
		pending, err := mockFossa.HasPendingInvitation("alice@example.com")
		require.NoError(t, err)
		assert.True(t, pending)
	})

	t.Run("mock FOSSA client creates teams", func(t *testing.T) {
		mockFossa := NewMockFossaClient()

		// Create team
		team, err := mockFossa.CreateTeam("test-project")
		require.NoError(t, err)
		assert.NotNil(t, team)
		assert.Equal(t, "test-project", team.Name)
		assert.Greater(t, team.ID, 0)

		// Verify tracking
		created := mockFossa.GetTeamsCreated()
		require.Len(t, created, 1)
		assert.Contains(t, created, "test-project")
	})

	t.Run("in-memory database works", func(t *testing.T) {
		db := setupTestDB(t)
		project, maintainers := seedProjectData(t, db)

		assert.Equal(t, "test-project", project.Name)
		require.Len(t, maintainers, 2)
		assert.Equal(t, "alice", maintainers[0].GitHubAccount)
		assert.Equal(t, "bob", maintainers[1].GitHubAccount)
	})

	t.Run("helper functions create proper events", func(t *testing.T) {
		event := createIssueLabeledEvent("kubernetes", "fossa", 123)
		assert.Equal(t, "labeled", event.GetAction())
		assert.Equal(t, "fossa", event.Label.GetName())
		assert.Equal(t, 123, event.Issue.GetNumber())
		assert.Contains(t, event.Issue.GetTitle(), "kubernetes")

		commentEvent := createIssueCommentEvent("prometheus", "/fossa-invite accepted", "alice", 456, []string{"alice", "bob"})
		assert.Equal(t, "created", commentEvent.GetAction())
		assert.Equal(t, "/fossa-invite accepted", commentEvent.Comment.GetBody())
		assert.Equal(t, "alice", commentEvent.Comment.User.GetLogin())
		assert.Len(t, commentEvent.Issue.Assignees, 2)
	})
}

func TestFossaChosen_Basic(t *testing.T) {
	t.Run("successful onboarding with new team", func(t *testing.T) {
		// Setup
		db := setupTestDB(t)
		project, _ := seedProjectData(t, db)

		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()

		server := createTestServer(t, db, mockFossa, mockGitHub)

		// Create fake issue event
		issueEvent := createIssueLabeledEvent(project.Name, "fossa", 42)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.fossaChosen(project.Name, req, issueEvent)

		// Verify FOSSA interactions
		teamsCreated := mockFossa.GetTeamsCreated()
		assert.Len(t, teamsCreated, 1, "Should create exactly one team")
		assert.Contains(t, teamsCreated, "test-project")

		invitationsSent := mockFossa.GetInvitationsSent()
		assert.Len(t, invitationsSent, 2, "Should send invitations to 2 maintainers")
		assert.Contains(t, invitationsSent, "alice@example.com")
		assert.Contains(t, invitationsSent, "bob@example.com")

		// Verify GitHub comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1, "Should create exactly one GitHub comment")

		comment := comments[0]
		assert.Equal(t, "cncf", comment.Owner)
		assert.Equal(t, "onboarding", comment.Repo)
		assert.Equal(t, 42, comment.IssueNumber)

		// Verify comment content
		assert.Contains(t, comment.Body, "maintainer-d CNCF FOSSA onboarding")
		assert.Contains(t, comment.Body, "test-project has 2 maintainers")
		assert.Contains(t, comment.Body, "has been created in FOSSA")
		assert.Contains(t, comment.Body, "/fossa-invite accepted")
		// Just change this part in the test:
		assert.Contains(t, comment.Body, "@alice")
		assert.Contains(t, comment.Body, "@bob")

		// Note: In current implementation, maintainer handles are not mentioned
		// when invitations are sent successfully (only on errors/special cases)
	})

	t.Run("maintainer already has pending invitation", func(t *testing.T) {
		// Setup
		db := setupTestDB(t)
		project, _ := seedProjectData(t, db)

		mockFossa := NewMockFossaClient()
		// Simulate alice already has pending invitation
		mockFossa.SendUserInvitation("alice@example.com")

		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, db, mockFossa, mockGitHub)

		issueEvent := createIssueLabeledEvent(project.Name, "fossa", 42)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.fossaChosen(project.Name, req, issueEvent)

		// Verify GitHub comment includes aggregated invitation summary
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@alice")
		assert.Contains(t, comments[0].Body, "Invitation(s) to join CNCF FOSSA sent to")
	})

	t.Run("maintainer already exists in FOSSA", func(t *testing.T) {
		// Setup
		db := setupTestDB(t)
		project, _ := seedProjectData(t, db)

		mockFossa := NewMockFossaClient()
		// Simulate alice already exists as FOSSA user
		mockFossa.SetUserExists("alice@example.com", true)

		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, db, mockFossa, mockGitHub)

		issueEvent := createIssueLabeledEvent(project.Name, "fossa", 42)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.fossaChosen(project.Name, req, issueEvent)

		// Verify GitHub comment mentions aggregated existing member info
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@alice")
		assert.Contains(t, comments[0].Body, "CNCF FOSSA Users added to the team as Team Admins")
	})
}

func TestSignProjectUpForFOSSA_CreateTeamFailure(t *testing.T) {
	db := setupTestDB(t)
	project, maintainers := seedProjectData(t, db)

	mockFossa := NewMockFossaClient()
	mockFossa.SetCreateTeamError(errors.New("boom"))

	for _, maintainer := range maintainers {
		mockFossa.SetUserExists(maintainer.Email, true)
	}

	mockGitHub := NewMockGitHubTransport()
	server := createTestServer(t, db, mockFossa, mockGitHub)

	assert.NotPanics(t, func() {
		_, err := server.signProjectUpForFOSSA(project)
		assert.Error(t, err)
	})
}

func TestLabelCommand(t *testing.T) {
	t.Run("successful label addition - fossa", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event with /label fossa command
		event := createIssueCommentEvent(project.Name, "/label fossa", "alice", 100, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 1)
		assert.Equal(t, "cncf", labels[0].Owner)
		assert.Equal(t, "onboarding", labels[0].Repo)
		assert.Equal(t, 100, labels[0].IssueNumber)
		assert.Contains(t, labels[0].Labels, "fossa")

		// Verify confirmation comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@alice")
		assert.Contains(t, comments[0].Body, "fossa")
		assert.Contains(t, comments[0].Body, "CNCF FOSSA")
	})

	t.Run("successful label addition - snyk", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event with /label snyk command
		event := createIssueCommentEvent(project.Name, "/label snyk", "bob", 101, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 1)
		assert.Contains(t, labels[0].Labels, "snyk")

		// Verify confirmation comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@bob")
		assert.Contains(t, comments[0].Body, "snyk")
		assert.Contains(t, comments[0].Body, "CNCF Snyk")
	})

	t.Run("unauthorized user", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event from unauthorized user
		event := createIssueCommentEvent(project.Name, "/label fossa", "unauthorized-user", 102, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify no label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 0)

		// Verify error comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@unauthorized-user")
		assert.Contains(t, comments[0].Body, "not yet been registered")
	})

	t.Run("invalid label name", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event with invalid label
		event := createIssueCommentEvent(project.Name, "/label invalid", "alice", 103, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify no label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 0)

		// Verify error comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "Invalid label")
		assert.Contains(t, comments[0].Body, "invalid")
		assert.Contains(t, comments[0].Body, "fossa")
		assert.Contains(t, comments[0].Body, "snyk")
	})

	t.Run("invalid command format", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event with invalid format
		event := createIssueCommentEvent(project.Name, "/label", "alice", 104, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify no label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 0)

		// Verify error comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "Invalid")
		assert.Contains(t, comments[0].Body, "/label fossa")
		assert.Contains(t, comments[0].Body, "/label snyk")
	})

	t.Run("project not found", func(t *testing.T) {
		// Setup - no project seeded
		database := setupTestDB(t)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event for non-existent project
		event := createIssueCommentEvent("non-existent-project", "/label fossa", "alice", 105, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify no label was added
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 0)

		// Verify error comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "not found")
		assert.Contains(t, comments[0].Body, "maintainer-d database")
	})

	t.Run("case insensitive label names", func(t *testing.T) {
		// Setup
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)
		mockFossa := NewMockFossaClient()
		mockGitHub := NewMockGitHubTransport()
		server := createTestServer(t, database, mockFossa, mockGitHub)

		// Create issue comment event with uppercase label
		event := createIssueCommentEvent(project.Name, "/label FOSSA", "alice", 106, nil)
		req, _ := http.NewRequest("POST", "/webhook", nil)

		// Execute
		server.handleLabelCommand(req, event)

		// Verify label was added (normalized to lowercase)
		labels := mockGitHub.GetAddedLabels()
		require.Len(t, labels, 1)
		assert.Contains(t, labels[0].Labels, "fossa")

		// Verify confirmation comment
		comments := mockGitHub.GetCreatedComments()
		require.Len(t, comments, 1)
		assert.Contains(t, comments[0].Body, "@alice")
		assert.Contains(t, comments[0].Body, "fossa")
	})
}

func TestFossaInviteAcceptedAuthorization(t *testing.T) {
	t.Run("staff member is authorized", func(t *testing.T) {
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)

		// Seed staff member
		f := model.Foundation{Name: "CNCF"}
		require.NoError(t, database.Create(&f).Error)
		staff := model.StaffMember{
			Name:          "Carol Staff",
			Email:         "carol@example.com",
			GitHubAccount: "carol",
			FoundationID:  &f.ID,
		}
		require.NoError(t, database.Create(&staff).Error)

		server := createTestServer(t, database, NewMockFossaClient(), NewMockGitHubTransport())

		ok := server.isAuthorizedForProjectAction("carol", project, &github.Issue{})
		assert.True(t, ok)
	})

	t.Run("non staff/non maintainer/non assignee is not authorized", func(t *testing.T) {
		database := setupTestDB(t)
		project, _ := seedProjectData(t, database)

		server := createTestServer(t, database, NewMockFossaClient(), NewMockGitHubTransport())
		ok := server.isAuthorizedForProjectAction("mallory", project, &github.Issue{})
		assert.False(t, ok)
	})
}
