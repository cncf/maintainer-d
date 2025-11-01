package main

import (
	"fmt"
	"maintainerd/plugins/fossa"
	"os"
)

const (
	apiTokenEnvVar = "FOSSA_API_TOKEN" //nolint:gosec
)

func main() {
	token := os.Getenv(apiTokenEnvVar)
	if token == "" {
		fmt.Fprintf(os.Stderr, "please set $%s\n", apiTokenEnvVar)
		os.Exit(1)
	}
	fossaClient := fossa.NewClient(token)

	teams, err := fossaClient.FetchTeams()

	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching teams: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Your teams:")
	for _, t := range teams {
		fmt.Printf("  %3d  %-20s  users:%3d  projects:%3d  releases:%3d\n",
			t.ID, t.Name,
			len(t.TeamUsers),
			t.TeamProjectsCount,
			t.TeamReleaseGroupsCount,
		)
	}

	// 2) pick a team ID on the CLI
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <teamName>\n", os.Args[0])
		os.Exit(1)
	}
	teamName := os.Args[1]

	teamID, err := fossaClient.GetTeamId(teams, teamName)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching team: %v\n", err)
		os.Exit(1)
	}

	emails, err := fossaClient.FetchTeamUserEmails(teamID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching users: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Team members’ emails:")
	for i, email := range emails {
		if i == 0 {
			fmt.Println(email)
		} else {
			fmt.Printf(", %s", email)
		}
	}
	fmt.Println()
}
