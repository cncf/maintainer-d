package onboarding

import (
	"net/http"
	"testing"

	"github.com/google/go-github/v55/github"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"maintainerd/db"
	"maintainerd/model"
)

// setupTestDB creates an in-memory SQLite database with schema for testing
func setupTestDB(t *testing.T) *gorm.DB {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = database.AutoMigrate(
		&model.Company{},
		&model.Foundation{},
		&model.Project{},
		&model.Maintainer{},
		&model.MaintainerProject{},
		&model.StaffMember{},
		&model.Service{},
		&model.ServiceTeam{},
		&model.AuditLog{},
	)
	require.NoError(t, err)

	// Create FOSSA service by default since most tests need it
	fossaService := model.Service{Name: "FOSSA"}
	database.Create(&fossaService)

	return database
}

// seedProjectData creates a test project with maintainers in the database
func seedProjectData(t *testing.T, database *gorm.DB) (model.Project, []model.Maintainer) {
	company := model.Company{Name: "Test Company"}
	require.NoError(t, database.Create(&company).Error)

	project := model.Project{Name: "test-project", Maturity: model.Graduated}
	require.NoError(t, database.Create(&project).Error)

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
		require.NoError(t, database.Create(&maintainers[i]).Error)
	}

	// Associate maintainers with project
	require.NoError(t, database.Model(&project).Association("Maintainers").Append(maintainers))

	return project, maintainers
}

// seedProjectWithService creates a project with an associated FOSSA service team
//
//lint:ignore U1000 This function will be used in future implementation
func seedProjectWithService(t *testing.T, database *gorm.DB, project model.Project, serviceTeamID int) model.ServiceTeam {
	// Create or get FOSSA service
	service := model.Service{Name: "FOSSA"}
	database.Where("name = ?", "FOSSA").FirstOrCreate(&service)

	serviceTeam := model.ServiceTeam{
		ServiceTeamID:   serviceTeamID,
		ServiceID:       service.ID,
		ServiceTeamName: stringPtr(project.Name),
		ProjectID:       project.ID,
		ProjectName:     stringPtr(project.Name),
	}
	require.NoError(t, database.Create(&serviceTeam).Error)

	return serviceTeam
}

// createTestServer creates a test EventListener with mocked dependencies
func createTestServer(t *testing.T, database *gorm.DB, mockFossa *MockFossaClient, mockGitHub *MockGitHubTransport) *EventListener {
	store := db.NewSQLStore(database)

	// Build projects map
	projectMap, err := store.GetProjectMapByName()
	require.NoError(t, err)

	httpClient := &http.Client{Transport: mockGitHub}
	ghClient := github.NewClient(httpClient)

	return &EventListener{
		Store:        store,
		FossaClient:  mockFossa,
		GitHubClient: ghClient,
		Projects:     projectMap,
		Secret:       []byte("test-secret"),
	}
}

// Helper functions for pointer types

func stringPtr(s string) *string {
	return &s
}

// GitHub event helper functions

// createIssueLabeledEvent creates a fake GitHub issue labeled event
//
//nolint:unparam
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

// createIssueCommentEvent creates a fake GitHub issue comment event
func createIssueCommentEvent(projectName, body, author string, issueNum int, assignees []string) *github.IssueCommentEvent {
	issue := &github.Issue{
		Number: github.Int(issueNum),
		Title:  github.String("[PROJECT ONBOARDING] " + projectName),
	}

	// Add assignees if provided
	if len(assignees) > 0 {
		issue.Assignees = make([]*github.User, len(assignees))
		for i, assignee := range assignees {
			issue.Assignees[i] = &github.User{Login: github.String(assignee)}
		}
	}

	return &github.IssueCommentEvent{
		Action: github.String("created"),
		Comment: &github.IssueComment{
			Body: github.String(body),
			User: &github.User{Login: github.String(author)},
		},
		Issue: issue,
		Repo: &github.Repository{
			Owner: &github.User{Login: github.String("cncf")},
			Name:  github.String("onboarding"),
		},
	}
}

// createWebhookRequest creates a mock HTTP request for webhook testing.
// TODO: This will be used when testing handleWebhook authorization logic.
//
//lint:ignore U1000 Reserved for future webhook handler tests
//nolint:unparam
func createWebhookRequest(t *testing.T, eventType string, payload []byte) *http.Request {
	req, err := http.NewRequest("POST", "/webhook", nil)
	require.NoError(t, err)

	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("Content-Type", "application/json")

	return req
}
