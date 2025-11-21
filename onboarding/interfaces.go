package onboarding

import "maintainerd/plugins/fossa"

// FossaClientInterface defines the interface for FOSSA client operations
type FossaClientInterface interface {
	CreateTeam(name string) (*fossa.Team, error)
	SendUserInvitation(email string) error
	HasPendingInvitation(email string) (bool, error)
	FetchTeamUserEmails(teamID int) ([]string, error)
	AddUserToTeamByEmail(teamID int, email string, roleID int) error
	FetchImportedRepos(teamID int) (int, fossa.ImportedProjects, error)
	ImportedProjectLinks(projects fossa.ImportedProjects) string
	FetchTeam(name string) (*fossa.Team, error)
	FetchTeams() ([]fossa.Team, error)
}
