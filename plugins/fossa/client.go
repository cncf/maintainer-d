package fossa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase                    = "https://app.fossa.com/api"
	ErrCodeInviteAlreadyExists = 2011
	ErrCodeUserAlreadyMember   = 2001
)

var (
	ErrTeamAlreadyExists   = errors.New("fossa: team already exists")
	ErrInviteAlreadyExists = errors.New("fossa: invitation already exists")
	ErrUserAlreadyMember   = errors.New("fossa: user is already a member")
)

type Client struct {
	APIKey  string
	APIBase string
}

func NewClient(token string) *Client {
	return &Client{
		APIKey:  token,
		APIBase: apiBase,
	}
}

// FetchFirstPageOfUsers returns an array of User or an error
func (c *Client) FetchFirstPageOfUsers() ([]User, error) {
	req, _ := http.NewRequest("GET", c.APIBase+"/users", nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FetchUsers failed: called $s\n\t\t%s – %s", resp.Status, string(body))
	}
	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}
	return users, nil
}
func (c *Client) FetchUsers() ([]User, error) {
	var allUsers []User
	page := 0
	count := 100 // Adjust this value as per FOSSA API limits
	fmt.Printf("")
	for {
		// Construct paginated URL
		usersEndpoint := fmt.Sprintf("%s/users?count=%d&page=%d", c.APIBase, count, page)

		req, err := http.NewRequest("GET", usersEndpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		// Read body early for error handling/logging
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("FetchUsers failed: %s\n\t\t%s", resp.Status, string(body))
		}

		var users []User
		if err := json.Unmarshal(body, &users); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allUsers = append(allUsers, users...)

		// If we got fewer users than count, we’re done
		if len(users) < count {
			break
		}
		page++
	}
	fmt.Printf("FetchUsers page: %d Found %d FOSSA Users\n", page, len(allUsers))
	return allUsers, nil
}

// FetchUserInvitations GETs /api/user-invitations - Retrieves all active (non-expired) user invitations for an
// organization
func (c *Client) FetchUserInvitations() (string, error) {
	req, _ := http.NewRequest("GET", c.APIBase+"/user-invitations", nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("FetchUserInvitations failed %s\n", err), err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("FetchUserInvitations failed: called $s\n\t\t%s – %s", resp.Status, string(body))
	}

	return string(body), nil
}

// HasPendingInvitation performs a check to see if an active invitation exists for the email.
// It relies on FetchUserInvitations and searches for the email within the response body to avoid
// coupling to an unstable API schema.
func (c *Client) HasPendingInvitation(email string) (bool, error) {
	log.Printf("HasPendingInvitation: email=%q", email)
	body, err := c.FetchUserInvitations()
	if err != nil {
		log.Printf("HasPendingInvitation: err=%q", err)
		return false, err
	}
	log.Printf("HasPendingInvitation: body=%q", body)
	// Case-insensitive substring search; avoids schema assumptions.
	return strings.Contains(strings.ToLower(body), strings.ToLower(email)), nil
}

// SendUserInvitation uses email to send an invitation to join this org of FOSSA
func (c *Client) SendUserInvitation(email string) error {
	payload := map[string]string{"email": email}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode body: %w", err)
	}

	// TODO - orgId hard coded write GetOrg
	req, err := http.NewRequest("POST", c.APIBase+"/organizations/"+"162"+"/invite", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("FetchUserInvitations failed %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var fossaErr Error
		if err := json.Unmarshal(body, &fossaErr); err != nil {
			return fmt.Errorf("SendUserInvitation calling json.Unmarshal failed for %s: %s\n\t\t%s", email, resp.Status, string(body))
		}

		switch fossaErr.Code {
		case ErrCodeInviteAlreadyExists:
			return fmt.Errorf("%w: %s", ErrInviteAlreadyExists, fossaErr.Message)
		case ErrCodeUserAlreadyMember:
			return fmt.Errorf("%w: %s", ErrUserAlreadyMember, fossaErr.Message)
		default:
			return fmt.Errorf("SendUserInvitation failed for %s (code %d): %s – %s",
				email, fossaErr.Code, resp.Status, fossaErr.Message)
		}
	}

	return nil
}

// FetchTeam retrieves a team by its name from the list of all teams or returns an error if the team is not found.
func (c *Client) FetchTeam(name string) (*Team, error) {
	teams, err := c.FetchTeams()
	if err != nil {
		return nil, fmt.Errorf("failed to find team with name %s, FOSSA Error was %v", name, err)
	}
	for _, team := range teams {
		if team.Name == name {
			return &team, nil
		}
	}
	return nil, fmt.Errorf("failed to find team with name %s", name)
}

// FetchTeams calls GET /api/teams
func (c *Client) FetchTeams() ([]Team, error) {
	req, _ := http.NewRequest("GET", c.APIBase+"/teams", nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list teams failed: %s – %s", resp.Status, string(body))
	}

	var teams []Team
	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		return nil, err
	}
	return teams, nil
}

// FetchTeamUserEmails calls GET /api/teams/{id}/members
func (c *Client) FetchTeamUserEmails(teamID int) ([]string, error) {
	var teamMemberEndpoint = fmt.Sprintf("%s/teams/%d/members", c.APIBase, teamID)
	req, _ := http.NewRequest("GET", teamMemberEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list team users failed: %s – %s", resp.Status, string(body))
	}
	var emails []string
	var members TeamMembers
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		return nil, fmt.Errorf("list team users failed json.NewDecoder returned: %s\nwhen trying to decode %s", err, resp.Body)
	}
	if members.TotalCount > 0 {
		for _, result := range members.Results {
			emails = append(emails, result.Email)
		}
	}
	return emails, nil
}

// AddUserToTeamByEmail attempts to add a user to a FOSSA team by email.
// If roleID is not 0, it will be included; otherwise the server default role is used.
// Returns ErrUserAlreadyMember for idempotent behavior when applicable.
func (c *Client) AddUserToTeamByEmail(teamID int, email string, roleID int) error {
	fmt.Printf("AddUserToTeamByEmail: teamID %d email %s, roleID %d\n", teamID, email, roleID)

	// The FOSSA API expects a bulk users payload to /teams/{id}/users with action=add.
	// We must provide user IDs, so resolve the user by email first.
	uid, err := c.findUserIDByEmail(email)
	fmt.Printf("AddUserToTeamByEmail: uid=%q, err=%v\n", uid, err)
	if err != nil {
		return fmt.Errorf("resolve user by email: %w", err)
	}

	bodyPayload := map[string]interface{}{
		"users": []map[string]interface{}{
			{
				"id": uid,
			},
		},
		"action": "add",
	}
	// Let's try defaulting the role
	// if roleID != 0 {
	//	bodyPayload["users"].([]map[string]interface{})[0]["roleId"] = roleID
	//}
	jsonBody, err := json.Marshal(bodyPayload)
	if err != nil {
		return fmt.Errorf("failed to encode body: %w", err)
	}
	fmt.Printf("AddUserToTeamByEmail: %s\n", bodyPayload)
	teamsUsersEndpoint := fmt.Sprintf("%s/teams/%d/users", c.APIBase, teamID)
	req, err := http.NewRequest("PUT", teamsUsersEndpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// Attempt to decode known FOSSA error schema
	var fossaErr Error
	if err := json.Unmarshal(body, &fossaErr); err == nil {
		switch fossaErr.Code {
		case ErrCodeUserAlreadyMember:
			return fmt.Errorf("%w: %s", ErrUserAlreadyMember, fossaErr.Message)
		default:
			return fmt.Errorf("AddUserToTeamByEmail failed (code %d): %s – %s", fossaErr.Code, resp.Status, fossaErr.Message)
		}
	}
	// Fallback: unknown error format
	return fmt.Errorf("AddUserToTeamByEmail failed: %s – %s", resp.Status, string(body))
}

// findUserIDByEmail searches the user list for a matching email and returns the user ID.
func (c *Client) findUserIDByEmail(email string) (int, error) {
	log.Printf("findUserIDByEmail: email=%q", email)
	users, err := c.FetchUsers()
	if err != nil {
		return 0, err
	}
	target := normalizeEmail(email)
	if target == "" {
		return 0, fmt.Errorf("user not found by email: %s", email)
	}

	for _, u := range users {
		if normalizeEmail(u.Email) == target {
			return u.ID, nil
		}
		if u.GitHub.Email != nil && normalizeEmail(*u.GitHub.Email) == target {
			return u.ID, nil
		}
		if u.Bitbucket.Email != nil && normalizeEmail(*u.Bitbucket.Email) == target {
			return u.ID, nil
		}
	}
	return 0, fmt.Errorf("user not found by email: %s", email)
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// GetTeamId searches a slice of Team objects by name.
// Returns the team’s ID if found, or an error “team not found” if not.
func (c *Client) GetTeamId(teams []Team, name string) (int, error) {
	for _, t := range teams {
		if t.Name == name {
			return t.ID, nil
		}
	}
	return 0, fmt.Errorf("team not found: %q", name)
}

// FetchTeamsMap returns a map of FOSSA Teams keyed by the name of the team
func (c *Client) FetchTeamsMap() (map[string]Team, error) {
	ta, err := c.FetchTeams()
	if err != nil {
		log.Printf("FOSSA client, FetchTeamsMap:Error fetching teams: %v", err)
		return nil, err
	}
	tm := map[string]Team{}

	for i, team := range ta {
		tm[team.Name] = ta[i]
	}
	return tm, nil
}

// GetTeam returns a *@Team object for the team called @name if it can be retrieved and exists on FOSSA or
// a nil Team and an error if FOSSA cannot find the team.
func (c *Client) GetTeam(teamID int) (*Team, error) {

	req, err := http.NewRequest("GET", c.APIBase+"/teams/"+strconv.Itoa(teamID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FetchTeams failed %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	var team Team
	if err := json.Unmarshal(body, &team); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &team, nil
}

func (c *Client) CreateTeam(name string) (*Team, error) {
	payload := map[string]string{"name": name}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode body: %w", err)
	}

	req, err := http.NewRequest("POST", c.APIBase+"/teams", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var fossaErr Error
		if err := json.Unmarshal(body, &fossaErr); err == nil {
			switch fossaErr.Code {
			case 2003:
				var team *Team
				if err := json.Unmarshal(body, &team); err != nil {
					return nil, fmt.Errorf("failed to decode response: %w", err)
				}
				team, err = c.FetchTeam(name)
				if err != nil {
					return nil, fmt.Errorf("CreateTeam: failed to fetch existing team after team-already-exists error: %w", err)
				}
				if team != nil {
					return team, nil // We disregard the team-already-exists error
				}
			default:
				return nil, fmt.Errorf("CreateTeam failed with FOSSA error code %d: %s – %s", fossaErr.Code, fossaErr.Name, fossaErr.Message)
			}
		}
		// Fallback: unknown error format
		return nil, fmt.Errorf("CreateTeam failed: %s\n\tResponse: %s", resp.Status, string(body))
	}

	var team Team
	if err := json.Unmarshal(body, &team); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &team, nil
}

// FetchImportedRepos is a function that returns an ImportedProjects struct for the FOSSA Team associated with teamID.
// returns the number of repos imported and the only first page of imported project records.
func (c *Client) FetchImportedRepos(teamID int) (int, ImportedProjects, error) {
	team, err := c.GetTeam(teamID)
	repoCount := 0
	if err != nil {
		return 0, ImportedProjects{}, fmt.Errorf("call to c.GetTeam(%d) returned %w", teamID, err)
	}
	if team == nil {
		return 0, ImportedProjects{}, fmt.Errorf("team not found %d", teamID)
	}
	req, err := http.NewRequest("GET", c.APIBase+"/teams/"+strconv.Itoa(teamID)+"/projects", nil)

	if err != nil {
		return 0, ImportedProjects{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return repoCount, ImportedProjects{}, fmt.Errorf("FetchImportedRepos failed %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return repoCount, ImportedProjects{}, fmt.Errorf("failed to read response body: %w", err)
	}
	var repos ImportedProjects
	if err := json.Unmarshal(body, &repos); err != nil {
		return repoCount, ImportedProjects{}, fmt.Errorf("failed to decode response: %w", err)
	}
	repoCount = repos.TotalCount
	return repoCount, repos, nil
}

type TeamMembers struct {
	Results []struct {
		UserID   int    `json:"userId"`
		RoleID   int    `json:"roleId"`
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"results"`
	PageSize   int `json:"pageSize"`
	Page       int `json:"page"`
	TotalCount int `json:"totalCount"`
}

// Team models a single team object from GET /api/teams
type Team struct {
	ID               int       `json:"id"`
	OrganizationID   int       `json:"organizationId"`
	Name             string    `json:"name"`
	DefaultRoleID    int       `json:"defaultRoleId"`
	AutoAddUsers     bool      `json:"autoAddUsers"`
	UniqueIdentifier string    `json:"uniqueIdentifier"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	TeamUsers        []struct {
		UserID int `json:"userId"`
		RoleID int `json:"roleId"`
	} `json:"teamUsers"`
	TeamReleaseGroupsCount int `json:"teamReleaseGroupsCount"`
	TeamProjectsCount      int `json:"teamProjectsCount"`
}

// User models the JSON returned by GET /api/users/{id}
type User struct {
	ID             int         `json:"id"`
	Username       string      `json:"username"`
	Email          string      `json:"email"`
	EmailVerified  bool        `json:"email_verified"`
	Demo           bool        `json:"demo"`
	Super          bool        `json:"super"`
	Joined         time.Time   `json:"joined"`
	LastVisit      time.Time   `json:"last_visit"`
	TermsAgreed    *time.Time  `json:"terms_agreed"`
	FullName       string      `json:"full_name"`
	Phone          string      `json:"phone"`
	Role           string      `json:"role"`
	OrganizationID int         `json:"organizationId"`
	SSOOnly        bool        `json:"sso_only"`
	Enabled        bool        `json:"enabled"`
	HasSetPassword *bool       `json:"has_set_password"`
	InstallAdmin   *bool       `json:"install_admin"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	UserRole       interface{} `json:"userRole"`
	Tokens         []struct {
		ID         int       `json:"id"`
		Name       string    `json:"name"`
		IsDisabled bool      `json:"isDisabled"`
		UpdatedAt  time.Time `json:"updatedAt"`
		CreatedAt  time.Time `json:"createdAt"`
		Meta       struct {
			PushOnly bool `json:"pushOnly"`
		} `json:"meta"`
	} `json:"tokens"`
	GitHub struct {
		Name      *string `json:"name"`
		Email     *string `json:"email"`
		AvatarURL string  `json:"avatar_url"`
	} `json:"github"`
	Bitbucket struct {
		Name      *string `json:"name"`
		Email     *string `json:"email"`
		AvatarURL string  `json:"avatar_url"`
	} `json:"bitbucketCloud"`
	TeamUsers []struct {
		RoleID int `json:"roleId"`
		Team   struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	} `json:"teamUsers"`
	Organization struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		AccessLevel string `json:"access_level"`
	} `json:"organization"`
}

type ImportedProjects struct {
	Results []struct {
		Title   string `json:"title"`
		Locator string `json:"locator"`
	} `json:"results"`
	PageSize   int `json:"pageSize"`
	Page       int `json:"page"`
	TotalCount int `json:"totalCount"`
}

type Error struct {
	UUID           string `json:"uuid"`
	Code           int    `json:"code"`
	Message        string `json:"message"`
	Name           string `json:"name"`
	HTTPStatusCode int    `json:"httpStatusCode"`
}

// ImportedProjectLinks for each imported project in projects takes the Title and Locator fields and uses them to create
// an unordered list of clickable projects in markdown format for use in GitHub Issue comments
func (c *Client) ImportedProjectLinks(projects ImportedProjects) string {
	if len(projects.Results) == 0 {
		return ""
	}

	var b strings.Builder
	for _, proj := range projects.Results {
		if proj.Title == "" {
			continue
		}

		link := formatLocator(proj.Locator)
		if link == "" {
			link = proj.Locator
		}

		fmt.Fprintf(&b, "- [%s](%s)\n", proj.Title, link)
	}

	return strings.TrimSpace(b.String())
}

func formatLocator(locator string) string {
	if locator == "" {
		return ""
	}

	loc := strings.TrimPrefix(locator, "git+")
	if !strings.HasPrefix(loc, "http") && !strings.Contains(loc, "://") {
		loc = "https://" + loc
	}

	u, err := url.Parse(loc)
	if err != nil || u.Host == "" {
		return ""
	}

	u.Host = strings.TrimSuffix(u.Host, ":")

	if !strings.HasSuffix(u.Path, ".git") && strings.HasSuffix(locator, ".git") {
		u.Path += ".git"
	}

	return u.String()
}
