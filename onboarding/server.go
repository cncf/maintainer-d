package onboarding

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maintainerd/model"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"google.golang.org/api/sourcerepo/v1"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/google/go-github/v55/github"
	"go.uber.org/zap"

	"maintainerd/db"
	"maintainerd/plugins/fossa"
)

// EventListener server that handles GitHub webhook events and triggers onboarding processes using the maintainerd db and
// known services such as FOSSA.
type EventListener struct {
	Store        *db.SQLStore
	FossaClient  *fossa.Client
	Secret       []byte
	Projects     map[string]model.Project
	Repo         sourcerepo.Repo
	GitHubClient *github.Client
}

func (s *EventListener) Init(dbPath, fossaAPItokenEnvVar, ghToken, org, repo string) error {
	dbConn, err := gorm.Open(sqlite.Open(dbPath))
	if err != nil {
		log.Printf("error: failed to connect to db: %v", err)
		return fmt.Errorf("connect to db: %w", err)
	}
	s.Store = db.NewSQLStore(dbConn)

	projectMap, err := s.Store.GetProjectMapByName()
	if err != nil {
		log.Printf("error: failed to get project map: %v", err)
		return fmt.Errorf("get project map: %w", err)
	}
	s.Projects = projectMap
	log.Printf("Init: DBG, project map has %d entries", len(s.Projects))
	log.Printf("Init: DBG, they are...")
	for _, project := range s.Projects {
		log.Printf("info: project: %s, projectID: %d, maintainer count: %d", project.Name, project.ID,
			len(project.Maintainers))
	}
	token := os.Getenv(fossaAPItokenEnvVar)
	if token == "" {
		log.Printf("Init: ERR, the environment variable %s must be set", fossaAPItokenEnvVar)
		return fmt.Errorf("missing required environment variable: %s", fossaAPItokenEnvVar)
	}
	s.FossaClient = fossa.NewClient(token)
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: ghToken})
	tc := oauth2.NewClient(context.Background(), ts)
	s.GitHubClient = github.NewClient(tc)

	log.Printf("info: EventListener initialized successfully for org %q and repo %q", org, repo)
	return nil
}

// Run starts an HTTP server listening on the given address.
func (s *EventListener) Run(addr string) error {
	http.HandleFunc("/webhook", s.handleWebhook)
	return http.ListenAndServe(addr, nil)
}

func (s *EventListener) handleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := github.ValidatePayload(r, s.Secret)
	if err != nil {
		log.Printf("handleWebhook: ERR github.ValidatePayload: %v", err)
		http.Error(w, "handleWebhook: github.ValidatePayload, invalid signature", http.StatusUnauthorized)
		return
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		http.Error(w, "handleWebhook: could not parse event", http.StatusBadRequest)
		return
	}

	switch e := event.(type) {
	case *github.IssueCommentEvent:
		// Only handle newly created comments
		if e.GetAction() != "created" {
			break
		}
		body := e.GetComment().GetBody()
		if body != "/fossa-invite accepted" {
			log.Printf("handleWebhook: WRN body does not have the command we are looking for: %v", body)
			break
		}
		// Determine project from issue title
		projectName, err := GetProjectNameFromProjectTitle(e.GetIssue().GetTitle())
		if err != nil {
			log.Printf("handleWebhook: WRN, could not parse project name from issue title: %v", err)
			break
		}
		project, ok := s.Projects[projectName]
		if !ok {
			log.Printf("handleWebhook: WRN, project %q not found in cache", projectName)
			break
		}

		// Authorization: allow project maintainers or CNCF Project Team handles (via env var list)
		actor := e.GetComment().GetUser().GetLogin()
		isAuthorized := false
		// Check if actor is a registered maintainer for this project
		if maintainers, err := s.Store.GetMaintainersByProject(project.ID); err == nil {
			for _, m := range maintainers {
				if m.GitHubAccount == actor {
					isAuthorized = true
					break
				}
			}
		}
		if !isAuthorized {
			// Also allow any GitHub handle assigned to the onboarding issue
			if issue := e.GetIssue(); issue != nil {
				// Check array of assignees
				for _, u := range issue.Assignees {
					if u.GetLogin() == actor {
						isAuthorized = true
						break
					}
				}
				// Fallback: single assignee field
				if !isAuthorized {
					if a := issue.GetAssignee(); a != nil && a.GetLogin() == actor {
						isAuthorized = true
					}
				}
			}
		}
		if !isAuthorized {
			// Post an authorization failure comment and return
			comment := "You are not authorized to perform this action."
			if err := s.updateIssue(r.Context(), e.GetRepo().GetOwner().GetLogin(), e.GetRepo().GetName(), e.GetIssue().GetNumber(), comment); err != nil {
				log.Printf("handleWebhook: WRN, failed to update GitHub issue: %v", err)
			}
			break
		}

		log.Printf("handleWebhook: INF, /fossa-invite accepted by @%s for project %q", actor, project.Name)

		// Ensure a FOSSA ServiceTeam exists for this project
		stMap, err := s.Store.GetProjectServiceTeamMap("FOSSA")
		if err != nil {
			log.Printf("handleWebhook: ERR, could not get FOSSA team map: %v", err)
			break
		}
		st, ok := stMap[project.ID]
		if !ok || st == nil || st.ServiceTeamID == 0 {
			// Team missing; do not create here per design. Inform via comment.
			msg := fmt.Sprintf("FOSSA team for project %q was not found. Please add the 'fossa' label to the onboarding issue to create the team, then re-run this command.", project.Name)
			if err := s.updateIssue(r.Context(), e.GetRepo().GetOwner().GetLogin(), e.GetRepo().GetName(), e.GetIssue().GetNumber(), msg); err != nil {
				log.Printf("handleWebhook: WRN, failed to update GitHub issue: %v", err)
			}
			break
		}

		// Process all maintainers: verify acceptance, check membership, add as Team Admin if needed
		actions, err := s.addProjectMaintainersToFossaTeam(r.Context(), project, st.ServiceTeamID)
		if err != nil {
			log.Printf("handleWebhook: ERR, addProjectMaintainersToFossaTeam: %v", err)
		}
		// Build and post summary comment (using GitHub handles only)
		var comment string
		comment += "### 🧪 maintainerd - CNCF FOSSA Team Membership Update\n\n"
		comment += fmt.Sprintf("Project: %s\n\n", project.Name)
		for _, a := range actions {
			comment += fmt.Sprintf("- %s\n", a)
		}
		if err != nil {
			comment += fmt.Sprintf("\nNote: encountered some errors: %v\n", err)
		}
		if err := s.updateIssue(r.Context(), e.GetRepo().GetOwner().GetLogin(), e.GetRepo().GetName(), e.GetIssue().GetNumber(), comment); err != nil {
			log.Printf("handleWebhook: WRN, failed to update GitHub issue: %v", err)
		}

	case *github.IssuesEvent:
		if e.GetAction() != "labeled" {
			break
		}
		issueTitle := e.Issue.GetTitle()
		issueUrl := e.Issue.GetURL()
		for _, label := range e.Issue.Labels {
			name := label.GetName()
			if name == "fossa" {
				log.Printf("handleWebhook: DBG, [%s](%s) lbl fossa", issueUrl, issueTitle)
				projectName, err := GetProjectNameFromProjectTitle(e.Issue.GetTitle())
				if err != nil {
					log.Printf("handleWebhook: WRN, could not parse project name [%s](%s) : %v",
						issueUrl, issueTitle, err)
					continue
				}

				log.Printf("handleWebhook: DBG, %s", projectName)

				// Get Project from db
				var project model.Project
				project = s.Projects[projectName]
				actions, err := signProjectUpForFOSSA(r.Context(), s.Store, s.FossaClient, project)
				if err != nil {
					log.Printf("handleWebhook: ERR, failed to send FOSSA invitations: %v", err)
				}

				// Format the steps as a Markdown comment
				var comment string
				comment += "###  🧪 maintainerd - CNCF FOSSA Onboarding Report\n\n" +
					"#### :spiral_notepad: Actions taken during onboarding...\n\n"
				for _, action := range actions {
					comment += fmt.Sprintf("- %s\n", action)
				}
				if err != nil {
					comment += fmt.Sprintf("\n❌ Onboarding encountered some problems: `%s`\n", err)
				} else {
					comment += "---\n\n" +
						"Once accepted:\n\n" +
						"- 👤 Add an issue comment _/fossa-invite accepted_ to this issue, we will then add you to you team as a **Team Admin** ([FOSSA RBAC](https://docs.fossa.com/docs/role-based-access-control#team-roles)).\n\n" +
						"- 📦 Then, _and only then_, can you start importing your code and documentation repositories into FOSSA: [Getting Started Guide](https://docs.fossa.com/docs/getting-started#importing-a-project).\n\n"
				}
				err = s.updateIssue(r.Context(), e.GetRepo().GetOwner().GetLogin(), e.GetRepo().GetName(), e.GetIssue().GetNumber(), comment)
				if err != nil {
					log.Printf("handleWebhook: WRN, failed to update GitHub issue: %v", err)
				} else {
					log.Printf("handleWebhook: INF, fossa comment added [%s](%s)", issueTitle, issueUrl)
				}
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// signProjectUpForFOSSA using @store, gets the maintainers registered for @project, uses @fc to email them FOSSA invites
// to their registered email addresses. As invitations are sent, we build up a list of actions that were taken by the
// process so that the client can report steps taken and their results; in actions we reference maintainers using their
// public GitHub account keeping their registered email addresses private.
func signProjectUpForFOSSA(context context.Context, store *db.SQLStore, fc *fossa.Client, project model.Project) ([]string, error) {
	var actions []string

	// Check for maintainers registered for this project
	maintainers, err := store.GetMaintainersByProject(project.ID)
	if err != nil {
		actions = append(actions, fmt.Sprintf(":x: %s maintainers are not yet registered.", project.Name))
		return actions, fmt.Errorf("signProjectUpForFOSSA: no maintainers found for project %v, project ID", project)
	}

	actions = append(actions, fmt.Sprintf("✅  %s has %d registered maintainers", project.Name, len(maintainers)))

	// Do we have a team already in FOSSA for @project?
	serviceTeams, err := store.GetProjectServiceTeamMap("FOSSA")
	if err != nil {
		actions = append(actions, fmt.Sprintf(":warning: Problem retrieving serviceTeams.  %v", err))
	}
	st, ok := serviceTeams[project.ID]
	if ok {
		actions = append(
			actions,
			fmt.Sprintf("👥 [%s team](https://app.fossa.com/account/settings/organization/teams/%d) was already in FOSSA",
				project.Name,
				st.ServiceTeamID))
	} else {
		// create the team on FOSSA, add the team to the ServiceTeams
		team, err := fc.CreateTeam(project.Name)

		if err != nil {
			actions = append(actions, fmt.Sprintf(":x: Problem creating team on FOSSA for %s: %v", project.Name, err))
		} else {
			log.Printf("team created: %s", team.Name)
			actions = append(actions,
				fmt.Sprintf("👥  [%s team](https://app.fossa.com/account/settings/organization/teams/%d) has been created in FOSSA",
					team.Name, team.ID))
			_, err := store.CreateServiceTeam(project.ID, project.Name, team.ID, team.Name)
			if err != nil {
				fmt.Printf("handleWebhook: WRN, failed to create service team: %v", err)
			}
		}
		if err != nil {
			fmt.Printf("signProjectUpForFOSSA: Error creating team on FOSSA for %s: %v", project.Name, err)
		}
	}
	if len(maintainers) == 0 {
		actions = append(actions, fmt.Sprintf("Maintainers not yet registered, for project %s", project.Name))
		return actions, fmt.Errorf(":x: no maintainers found for project %d", project.ID)
	}
	for _, maintainer := range maintainers {
		err := fc.SendUserInvitation(maintainer.Email) // TODO See if I can Name the User on FOSSA!

		if errors.Is(err, fossa.ErrInviteAlreadyExists) {
			actions = append(actions, fmt.Sprintf("@%s : you have a pending invitation to join CNCF FOSSA. Please check your registered email and accept the invitation within 48 hours.", maintainer.GitHubAccount))
		} else if errors.Is(err, fossa.ErrUserAlreadyMember) {
			// TODO Edge case - maintainers who are already signed up in FOSSA on another project.
			log.Printf("user is already a member, skipping")
		} else if err != nil {
			log.Printf("error sending invite: %v", err)
			actions = append(actions, fmt.Sprintf("@%s : there was a problem sending a CNCF FOSSA invitation to you.", maintainer.GitHubAccount))
		}
	}

	return actions, nil
}

func (s *EventListener) updateIssue(ctx context.Context, owner, repo string, issueNumber int, comment string) error {
	issueComment := &github.IssueComment{
		Body: github.String(comment),
	}
	_, _, err := s.GitHubClient.Issues.CreateComment(ctx, owner, repo, issueNumber, issueComment)
	if err != nil {
		log.Printf("updateIssue: ERR, error creating comment: %v", err)
	}
	return err
}

// splitAndTrim splits a comma-separated list and trims whitespace.
func splitAndTrim(s string) []string {
	log.Printf("splitAndTrim: s=%q", s)
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if t := trimWS(cur); t != "" {
				out = append(out, t)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if t := trimWS(cur); t != "" {
		out = append(out, t)
	}
	return out
}

func trimWS(s string) string {
	log.Printf("trimWS: s=%q", s)
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

// addProjectMaintainersToFossaTeam processes all registered maintainers for a project against the given FOSSA team.
// It does not include email addresses in returned action strings; only GitHub handles.
func (s *EventListener) addProjectMaintainersToFossaTeam(ctx context.Context, project model.Project, teamID int) ([]string, error) {
	log.Printf("addProjectMaintainersToFossaTeam: project=%q projectID=%d teamID=%d", project.Name, project.ID, teamID)
	var actions []string

	maintainers, err := s.Store.GetMaintainersByProject(project.ID)
	if err != nil {
		return nil, fmt.Errorf("GetMaintainersByProject: %w", err)
	}
	if len(maintainers) == 0 {
		actions = append(actions, "No registered maintainers found for this project")
		return actions, nil
	}

	// Get current team member emails once
	existingEmails, err := s.FossaClient.FetchTeamUserEmails(teamID)
	if err != nil {
		return actions, fmt.Errorf("FetchTeamUserEmails: %w", err)
	}

	// Optional: role for Team Admin from env; else omit to use default
	var roleIDPtr int = 3 // FIXME Hardcoding this for now

	// Iterate maintainers
	for _, m := range maintainers {
		handle := m.GitHubAccount
		email := m.Email
		// Verify acceptance: ensure no pending invitation for email
		pending, pendErr := s.FossaClient.HasPendingInvitation(email)
		if pendErr != nil {
			log.Printf("addProjectMaintainersToFossaTeam: WRN, checking pending invite for %s: %v", handle, pendErr)
		}
		if pending {
			actions = append(actions, fmt.Sprintf("@%s: invitation still pending; skipped", handle))
			continue
		}
		// Check membership
		if containsEmail(existingEmails, email) {
			actions = append(actions, fmt.Sprintf("@%s: already a member; no action", handle))
			continue
		}
		// Attempt to add to team as Team Admin (if role provided)
		if err := s.FossaClient.AddUserToTeamByEmail(teamID, email, roleIDPtr); err != nil {
			if errors.Is(err, fossa.ErrUserAlreadyMember) {
				actions = append(actions, fmt.Sprintf("@%s: already a member; no action", handle))
				continue
			}
			actions = append(actions, fmt.Sprintf("@%s: error adding to team; please retry or contact support", handle))
			log.Printf("addProjectMaintainersToFossaTeam: ERR, add user @%s: %v", handle, err)
			continue
		}
		actions = append(actions, fmt.Sprintf("@%s: added to FOSSA team %s as Team Admin", handle, project.Name))
		// Write audit log (best-effort)
		// NOTE: ServiceID is optional; we omit or could set to FOSSA ID if available.
		if s.Store != nil {
			lg := zapNewNopSugar()
			_ = s.Store.LogAuditEvent(lg, model.AuditLog{
				ProjectID:    project.ID,
				MaintainerID: &m.ID,
				Action:       "FOSSA_ADD_MEMBER",
				Message:      fmt.Sprintf("Added @%s to FOSSA team %s", handle, project.Name),
			})
		}
		// Update local cache of existing emails to avoid re-adding in this run
		existingEmails = append(existingEmails, email)
	}
	return actions, nil
}

func containsEmail(list []string, target string) bool {
	log.Printf("containsEmail: target=%q list_len=%d", target, len(list))
	for _, e := range list {
		if e == target {
			return true
		}
	}
	return false
}

// zapNewNopSugar returns a no-op SugaredLogger.
func zapNewNopSugar() *zap.SugaredLogger {
	log.Printf("zapNewNopSugar: called")
	// Inline minimal no-op sugar to avoid adding zap imports here
	// We cannot import zap in this file without adding the module; use a tiny shim via log.Printf only
	// However, Store.LogAuditEvent requires *zap.SugaredLogger; to satisfy type, we implement a minimal shim.
	// Fallback: use zap.NewNop().Sugar() via a thin wrapper declared in a small local package would be ideal.
	// To keep changes minimal, we avoid heavy logging here and pass a nil-safe substitute.
	return zap.NewNop().Sugar()
}
