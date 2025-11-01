package main

import (
	"log"
	"maintainerd/db"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	apiTokenEnvVar = "FOSSA_API_TOKEN"
	defaultDBPath  = "maintainers.db"
)

func main() {
	var dbPath string
	var projectName string
	var serviceName string

	rootCmd := &cobra.Command{
		Use:   "status",
		Short: "onboarding service for maintainerd",
	}

	onboardCmd := &cobra.Command{
		Use:   "onboard",
		Short: "Checks the Onboarding status of a project wrt FOSSA",
		Run: func(cmd *cobra.Command, args []string) {
			if projectName == "" {
				log.Fatal("ERROR: --project flag is required")
			}

			fossaToken := viper.GetString(apiTokenEnvVar)
			if fossaToken == "" {
				log.Fatalf("ERROR: environment variable %s is not set", apiTokenEnvVar)
			}

			newLogger := logger.New(
				log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
				logger.Config{
					SlowThreshold:             time.Second,   // Slow SQL threshold
					LogLevel:                  logger.Silent, // Log level
					IgnoreRecordNotFoundError: true,          // Ignore ErrRecordNotFound error for logger
					ParameterizedQueries:      true,          // Don't include params in the SQL log
					Colorful:                  false,         // Disable color
				},
			)

			dbSession, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
				Logger: newLogger,
			})
			if err != nil {
				log.Fatalf("failed to connect to database: %v", err)
			}
			s := db.NewSQLStore(dbSession)

			allProjects, err := s.GetProjectMapByName()
			if err != nil {
				log.Fatalf("failed to get  project list from db %v", err)
			} else {
				log.Printf("Found %d projects in db", len(allProjects))
			}
			project := allProjects[projectName]
			log.Printf("Status of %#v", project)
			maintainers, err := s.GetMaintainersByProject(project.ID)

			if err != nil {
				log.Fatalf("failed to get maintainers for project '%s': %v", projectName, err)
			} else {
				log.Printf("Found %d maintainers in db", len(maintainers))
			}

			if len(maintainers) == 0 {
				log.Fatalf("No maintainers found for project '%s'", projectName)
			}

			log.Printf("Onboarding project '%s' to service '%s'", projectName, serviceName)
			serviceTeam, err := s.GetServiceTeamByProject(project.ID, 1)
			if err != nil {
				log.Fatalf("failed to get service team for project '%s' and service '%s': %v", projectName, serviceName, err)
			}
			log.Printf("Service Team: %#v", serviceTeam)
		},
	}

	onboardCmd.Flags().StringVar(&projectName, "project", "", "Project name to onboard (required)")
	onboardCmd.Flags().StringVar(&serviceName, "service", "fossa", "Service name (default: fossa)")
	onboardCmd.MarkFlagRequired("project")

	rootCmd.AddCommand(onboardCmd)
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDBPath, "Path to SQLite database file")

	viper.AutomaticEnv() // binds environment variables to viper config

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("command failed: %v", err)
	}
}

//func reportProjectStatus(projectName string) {
//	var actions []string
//
//	// Check for maintainers registered for this project
//	maintainers, err := s.Store.GetMaintainersByProject(project.ID)
//	if err != nil {
//		actions = append(actions, fmt.Sprintf(":x: %s maintainers not present in db, @cncf-projects-team check maintainer-d db", project.Name))
//	}
//
//	actions = append(actions, fmt.Sprintf("✅  %s has %d maintainers registered in maintainer-d", project.Name, len(maintainers)))
//
//	// Do we have a team already in FOSSA for @project?
//	serviceTeams, err := s.Store.GetProjectServiceTeamMap("FOSSA")
//	if err != nil {
//		actions = append(actions, fmt.Sprintf(":warning: Problem retrieving serviceTeams.  %v", err))
//	}
//	st, ok := serviceTeams[project.ID]
//	if ok {
//		actions = append(
//			actions,
//			fmt.Sprintf("👥 [%s team](https://app.fossa.com/account/settings/organization/teams/%d) was already in FOSSA",
//				project.Name,
//				st.ServiceTeamID))
//	} else {
//		// create the team on FOSSA, add the team to the ServiceTeams
//		team, err := s.FossaClient.CreateTeam(project.Name)
//
//		if err != nil {
//			actions = append(actions, fmt.Sprintf(":x: Problem creating team on FOSSA for %s: %v", project.Name, err))
//		} else {
//			log.Printf("team created: %s", team.Name)
//			actions = append(actions,
//				fmt.Sprintf("👥  [%s team](https://app.fossa.com/account/settings/organization/teams/%d) has been created in FOSSA",
//					team.Name, team.ID))
//			_, err := s.Store.CreateServiceTeam(project.ID, project.Name, team.ID, team.Name)
//			if err != nil {
//				fmt.Printf("handleWebhook: WRN, failed to create service team: %v", err)
//			}
//		}
//		if err != nil {
//			fmt.Printf("signProjectUpForFOSSA: Error creating team on FOSSA for %s: %v", project.Name, err)
//		}
//	}
//	if len(maintainers) == 0 {
//		actions = append(actions, fmt.Sprintf("Maintainers not yet registered, for project %s", project.Name))
//		return actions, fmt.Errorf(":x: no maintainers found for project %d", project.ID)
//	}
//	for _, maintainer := range maintainers {
//		err := s.FossaClient.SendUserInvitation(maintainer.Email) // TODO See if I can Name the User on FOSSA!
//
//		if errors.Is(err, fossa.ErrInviteAlreadyExists) {
//			actions = append(actions, fmt.Sprintf("@%s : you have a pending invitation to join CNCF FOSSA. Please check your registered email and accept the invitation within 48 hours.", maintainer.GitHubAccount))
//		} else if errors.Is(err, fossa.ErrUserAlreadyMember) {
//			// TODO Edge case - maintainers already signed up to CNCF FOSSA, maintainer on an another project?
//			actions = append(actions, fmt.Sprintf("@%s : You are CNCF FOSSA User", maintainer.GitHubAccount))
//			// TODO call fc.AddUserToTeamByEmail()
//			log.Printf("user is already a member, skipping")
//		} else if err != nil {
//			log.Printf("error sending invite: %v", err)
//			actions = append(actions, fmt.Sprintf("@%s : there was a problem sending a CNCF FOSSA invitation to you.", maintainer.GitHubAccount))
//		}
//	}
//
//	// check if the project team has imported their repos. If we label an onboarding issue with 'fossa' and the project
//	// has been manually setup in the past, better to report that repos have been imported into FOSSA.
//	teamMap, err := s.Store.GetProjectServiceTeamMap("FOSSA")
//	if err != nil {
//		return nil, err
//	}
//
//	count, repos, err := s.FossaClient.FetchImportedRepos(teamMap[project.ID].ServiceTeamID)
//	importedRepos := s.FossaClient.ImportedProjectLinks(repos)
//	if count == 0 {
//		actions = append(actions, fmt.Sprintf("The %s project has not yet imported repos", project.Name))
//	} else {
//		actions = append(actions, fmt.Sprintf("The %s project team have imported %d repo(s)<BR>%s", project.Name, count, importedRepos))
//	}
//
//	return actions, nil
//
//}
