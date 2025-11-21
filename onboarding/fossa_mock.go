package onboarding

import (
	"errors"
	"maintainerd/plugins/fossa"
	"sync"
)

// MockFossaClient simulates FOSSA API behavior for testing
type MockFossaClient struct {
	mu            sync.Mutex
	teams         map[string]*fossa.Team
	invitations   map[string]bool  // email -> pending
	teamMembers   map[int][]string // teamID -> emails
	userExists    map[string]bool  // email -> exists
	userIDs       map[string]int   // email -> userID
	nextTeamID    int
	nextUserID    int
	importedRepos map[int]fossa.ImportedProjects // teamID -> imported projects

	// Capture calls for verification
	invitationsSent []string
	teamsCreated    []string
	membersAdded    map[int][]string // teamID -> emails added
}

// NewMockFossaClient creates a new mock FOSSA client
func NewMockFossaClient() *MockFossaClient {
	return &MockFossaClient{
		teams:         make(map[string]*fossa.Team),
		invitations:   make(map[string]bool),
		teamMembers:   make(map[int][]string),
		userExists:    make(map[string]bool),
		userIDs:       make(map[string]int),
		membersAdded:  make(map[int][]string),
		importedRepos: make(map[int]fossa.ImportedProjects),
		nextTeamID:    1000,
		nextUserID:    5000,
	}
}

// CreateTeam creates a new team in the mock
func (m *MockFossaClient) CreateTeam(name string) (*fossa.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.teams[name]; exists {
		return nil, fossa.ErrTeamAlreadyExists
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

// SendUserInvitation sends an invitation to a user
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

// HasPendingInvitation checks if a user has a pending invitation
func (m *MockFossaClient) HasPendingInvitation(email string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invitations[email], nil
}

// FetchTeamUserEmails returns all user emails for a team
func (m *MockFossaClient) FetchTeamUserEmails(teamID int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	emails, ok := m.teamMembers[teamID]
	if !ok {
		return nil, errors.New("team not found")
	}
	return append([]string{}, emails...), nil
}

// AddUserToTeamByEmail adds a user to a team
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

// FetchImportedRepos returns imported repos for a team
func (m *MockFossaClient) FetchImportedRepos(teamID int) (int, fossa.ImportedProjects, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repos, ok := m.importedRepos[teamID]
	if !ok {
		// Return empty by default
		return 0, fossa.ImportedProjects{Results: []struct {
			Title   string `json:"title"`
			Locator string `json:"locator"`
		}{}}, nil
	}
	return len(repos.Results), repos, nil
}

// ImportedProjectLinks returns formatted project links
func (m *MockFossaClient) ImportedProjectLinks(projects fossa.ImportedProjects) string {
	if len(projects.Results) == 0 {
		return ""
	}

	var links string
	for _, proj := range projects.Results {
		links += proj.Title + " "
	}
	return links
}

// FetchTeam returns a team by name
func (m *MockFossaClient) FetchTeam(name string) (*fossa.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	team, ok := m.teams[name]
	if !ok {
		return nil, errors.New("team not found")
	}
	return team, nil
}

// FetchTeams returns all teams
func (m *MockFossaClient) FetchTeams() ([]fossa.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	teams := make([]fossa.Team, 0, len(m.teams))
	for _, team := range m.teams {
		teams = append(teams, *team)
	}
	return teams, nil
}

// Test helper methods

// SetUserExists sets whether a user exists in FOSSA
func (m *MockFossaClient) SetUserExists(email string, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userExists[email] = exists
	if exists && m.userIDs[email] == 0 {
		m.userIDs[email] = m.nextUserID
		m.nextUserID++
	}
}

// AcceptInvitation simulates a user accepting an invitation
func (m *MockFossaClient) AcceptInvitation(email string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.invitations, email)
	m.userExists[email] = true
	if m.userIDs[email] == 0 {
		m.userIDs[email] = m.nextUserID
		m.nextUserID++
	}
}

// SetImportedRepos sets imported repos for a team
func (m *MockFossaClient) SetImportedRepos(teamID int, repos fossa.ImportedProjects) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.importedRepos[teamID] = repos
}

// GetInvitationsSent returns all emails that invitations were sent to
func (m *MockFossaClient) GetInvitationsSent() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.invitationsSent...)
}

// GetTeamsCreated returns all team names that were created
func (m *MockFossaClient) GetTeamsCreated() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.teamsCreated...)
}

// GetMembersAdded returns all emails added to a specific team
func (m *MockFossaClient) GetMembersAdded(teamID int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.membersAdded[teamID]...)
}

// Reset clears all state
func (m *MockFossaClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.teams = make(map[string]*fossa.Team)
	m.invitations = make(map[string]bool)
	m.teamMembers = make(map[int][]string)
	m.userExists = make(map[string]bool)
	m.userIDs = make(map[string]int)
	m.invitationsSent = nil
	m.teamsCreated = nil
	m.membersAdded = make(map[int][]string)
	m.importedRepos = make(map[int]fossa.ImportedProjects)
}
