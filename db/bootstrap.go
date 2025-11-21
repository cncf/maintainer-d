package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maintainerd/model"
	"maintainerd/plugins/fossa"
	"os"
	"strings"
	"time"

	"gorm.io/gorm/logger"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	StatusHdr            string = "Status"
	ProjectHdr           string = "Project"
	MaintainerNameHdr    string = "Maintainer Name"
	CompanyNameHdr       string = "Company"
	EmailHdr             string = "Emails"
	GitHubHdr            string = "Github Name"
	GitHubEmail          string = "GitHub Email"
	ParentProjectHdr     string = "Parent Project"
	MaintainerFileRefHdr string = "OWNERS/MAINTAINERS"
	MailingListAddrHdr   string = "Mailing List Address"
)

func BootstrapSQLite(dbPath, spreadsheetID, worksheetCredentialsPath, fossaToken string, seed bool) (*gorm.DB, error) {
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
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: newLogger,
	})
	// var s Store = NewSQLStore(db)
	if err != nil {
		return nil, fmt.Errorf("failed to open DB: %w", err)
	}

	if err := db.AutoMigrate(
		&model.Company{},
		&model.Project{},
		&model.Maintainer{},
		&model.Collaborator{},
		&model.MaintainerProject{},
		&model.Service{},
		&model.ServiceTeam{},
		&model.ServiceUser{},
		&model.ServiceUserTeams{},
	); err != nil {
		return nil, fmt.Errorf("auto-migration failed: %w", err)
	}

	if !seed {
		log.Println("bootstrap: database schema created but no seed data loaded")
		return db, nil
	}

	services := []model.Service{
		{Name: "FOSSA", Description: "Static code check we use to ensure 3rd Party License Policy"},
		{Name: "Service Desk", Description: "Jira"},
		{Name: "cncf.groups.io", Description: "Mailing list channels"},
		{Name: "Snyk", Description: "Static code checker for 3rd Party License Policy monitoring and compliance"},
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, service := range services {
			if err := tx.FirstOrCreate(&service, model.Service{Name: service.Name}).Error; err != nil {
				return fmt.Errorf("bootstrap: failed to insert service %s: %w", service.Name, err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := loadMaintainersAndProjects(db, spreadsheetID, worksheetCredentialsPath); err != nil {
		return nil, fmt.Errorf("bootstrap: failed to load maintainers and projects: %w", err)
	}

	//fossaService := model.Service{Model: gorm.Model{ID: 1}, Name: "FOSSA"}
	if err := loadFOSSA(db, fossaToken); err != nil {
		return nil, fmt.Errorf("bootstrap: failed to load FOSSA projects: %w", err)
	}

	log.Printf("bootstrap: completed and loaded seed data into %s", dbPath)
	return db, nil
}

// Reads data from spreadsheetID inserts it into db.
func loadMaintainersAndProjects(db *gorm.DB, spreadsheetID, credentialsPath string) error {
	ctx := context.Background()

	srv, err := sheets.NewService(
		ctx,
		option.WithCredentialsFile(credentialsPath),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope),
	)

	if err != nil {
		log.Fatalf("maintainerd: backend: loadMaintainersAndProjects: unable to retrieve Sheets client: %v", err)
		return err
	}

	rows, err := readSheetRows(ctx, srv, spreadsheetID)

	if err != nil {
		log.Fatalf("maintainerd-backend: loadMaintainersAndProjects - readSheetRows: %v", err)
		return err
	}

	var currentMaintainerRef string
	var currentMailingList string

	for _, row := range rows {
		var missingMaintainerFields []string
		log.Printf("TRACE, reading row, %v\n", row)
		name := row[MaintainerNameHdr]

		if name == "" {
			missingMaintainerFields = append(missingMaintainerFields, ":"+MaintainerNameHdr)
		}

		company := row[CompanyNameHdr]
		if company == "" {
			missingMaintainerFields = append(missingMaintainerFields, ":"+CompanyNameHdr)
		}

		email := row[EmailHdr]
		if email == "" {
			missingMaintainerFields = append(missingMaintainerFields, ":"+EmailHdr)
		}

		github := row[GitHubHdr]
		if github == "" {
			missingMaintainerFields = append(missingMaintainerFields, ":"+GitHubHdr)
		}
		githubEmail := row[GitHubEmail]
		if github == "" {
			missingMaintainerFields = append(missingMaintainerFields, ":"+GitHubHdr)
		}
		log.Printf("DEBUG, processing maintainer %s, missing fields %v \n", row[MaintainerNameHdr], missingMaintainerFields)
		var parent model.Project
		if parentName := row[ParentProjectHdr]; parentName != "" {
			if err := db.Where("name = ?", parentName).
				First(&parent).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					log.Printf("WARN, parent project '%s' not found for project '%s', importing without parent", parentName, row[ProjectHdr])
				} else {
					log.Printf("ERR, error looking up parent project '%s' for project '%s': %v", parentName, row[ProjectHdr], err)
				}
			} else {
				log.Printf("INFO, project '%s' will be associated with parent project '%s' (ID: %d)", row[ProjectHdr], parentName, parent.ID)
			}
		}
		currentMaintainerRef = row[MaintainerFileRefHdr]
		currentMailingList = row[MailingListAddrHdr]

		if err := db.Transaction(func(tx *gorm.DB) error {
			var project model.Project
			if parent.Name == "" {
				project = model.Project{
					Name:          row[ProjectHdr],
					Maturity:      model.Maturity(row[StatusHdr]),
					MaintainerRef: currentMaintainerRef,
					MailingList:   &currentMailingList,
				}
			} else {
				project = model.Project{
					Name:            row[ProjectHdr],
					Maturity:        parent.Maturity,
					MaintainerRef:   currentMaintainerRef,
					MailingList:     &currentMailingList,
					ParentProjectID: &parent.ID,
				}
			}
			if err := tx.FirstOrCreate(&project, model.Project{Name: project.Name}).Error; err != nil {
				return fmt.Errorf("ERR, loadMaintainersAndProjects - failed calling FirstOrCreate on project %v: error %v", project, err)
			}
			company := model.Company{Name: company}
			if err := tx.FirstOrCreate(&company, model.Company{Name: company.Name}).Error; err != nil {
				return fmt.Errorf("ERR, loadMaintainersAndProjects - failed calling FirstOrCreate on company %v: error %v", company, err)
			}
			maintainer := model.Maintainer{
				Name:             name,
				GitHubAccount:    github,
				GitHubEmail:      githubEmail,
				Email:            email,
				CompanyID:        &company.ID,
				MaintainerStatus: model.ActiveMaintainer,
			}
			if err := tx.FirstOrCreate(&maintainer, model.Maintainer{Email: maintainer.Email}).Error; err != nil {
				return fmt.Errorf("ERR, loadMaintainersAndProjects - failed calling FirstOrCreate on maintainer %v: error %v", maintainer, err)
			}
			// Ensure the association (in case the maintainer existed already)
			return tx.Model(&maintainer).
				Association("Projects").
				Append(&project)
		}); err != nil {
			log.Printf("WARN, loadMaintainersAndProjects Database transaction not committed, row skipped %v : error %v ", row, err)
		}
	}
	return nil
}

// readSheetRows returns every row as a map keyed by the header row and carries forward the last non‐empty Project and
// Status values when those cells are blank or missing.
// The readRange must include the header row.
// The
func readSheetRows(ctx context.Context, srv *sheets.Service, spreadsheetID string) ([]map[string]string, error) {
	resp, err := srv.Spreadsheets.Values.
		Get(spreadsheetID, "Active!A:J").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("db: Using %s unable to retrieve worksheet data: %w", spreadsheetID, err)
	}
	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("db: %s worksheet is empty", spreadsheetID)
	}

	// First row → headers
	headers := make([]string, len(resp.Values[0]))
	for i, cell := range resp.Values[0] {
		headers[i] = strings.TrimSpace(fmt.Sprint(cell))
	}

	// Find the column indexes for "Project" and "Status"
	projIdx, statIdx := -1, -1
	for i, h := range headers {
		switch h {
		case "Project":
			projIdx = i
		case "Status":
			statIdx = i
		}
	}

	var rows []map[string]string
	var lastProject, lastStatus string

	// Remaining rows → maps
	for _, r := range resp.Values[1:] {
		rowMap := make(map[string]string, len(headers))

		for i, h := range headers {
			// read raw cell if present
			var cellVal string
			if i < len(r) {
				cellVal = strings.TrimSpace(fmt.Sprint(r[i]))
			}

			switch i {
			case projIdx:
				if cellVal != "" {
					lastProject = cellVal
				}
				rowMap[h] = lastProject

			case statIdx:
				if cellVal != "" {
					lastStatus = cellVal
				}
				rowMap[h] = lastStatus

			default:
				rowMap[h] = cellVal
			}
		}

		rows = append(rows, rowMap)
	}

	return rows, nil
}

// loadFOSSA synchronizes all data in CNCF FOSSA
func loadFOSSA(db *gorm.DB, token string) error {
	users, teams, err := FetchFossaData(token)
	if err != nil {
		return fmt.Errorf("loadFOSSA: fetching FOSSA data: %s", err)
	}
	log.Printf("INF, FetchFossaData found %d users, and %d teams\n", len(users), len(teams))

	for _, user := range users {
		var maintainer *model.Maintainer     // A registered maintainer
		var collaborator *model.Collaborator // A contributor who has been signed up
		var su *model.ServiceUser
		ghName := safeGitHubName(user.GitHub.Name)
		if su, err = FirstOrCreateServiceUser(db, user); err != nil {
			log.Printf("ERR, FirstOrCreateServiceUser, error creating service user for %s: %v", user.Email, err)
		}
		if su == nil {
			log.Fatalf("ERR, FirstOrCreateServiceUser, service user for %s is nil! Exiting!", user.Email)
		}

		if maintainer = MapFossaUserToMaintainer(db, user.Email, ghName); maintainer != nil {
			log.Printf("INFO, MapFossaUserToMaintainer: %s was not used for maintainer registration", user.Email)
		} else {
			if collaborator = MapFossaUserCollaborator(db, user.Email, ghName, user); collaborator == nil {
				log.Printf("ERR, MapFossaUserCollaborator: error mapping service user using %s: %v", user.Email, err)
			}
		}
		st, err := CreateServiceTeamsForUser(db, user.TeamUsers)
		if err != nil {
			log.Printf("ERR, CreateServiceTeamsForUser failed for user %d (%s): %v", user.ID, user.Email, err)
			continue
		}

		if err := LinkServiceUserToTeam(db, su, st, maintainer, collaborator); err != nil {
			log.Printf("ERR, LinkServiceUserToTeam failed for user %d (%s): %v", user.ID, user.Email, err)
		}
	}

	return nil
}

// CreateServiceTeamsForUser takes a @db connection, and an array of FOSSA TeamUsers and adds them to the DB.
func CreateServiceTeamsForUser(
	db *gorm.DB,
	teamUsers []struct {
		RoleID int `json:"roleId"`
		Team   struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	},
) ([]*model.ServiceTeam, error) {

	var teams []*model.ServiceTeam
	var errMessages []string
	s := NewSQLStore(db)
	projects, err := s.GetProjectMapByName()
	if err != nil {
		return nil, fmt.Errorf("CreateServiceTeamsForUser: GetProjectMapByName failed to get project map: %v", err)
	}
	for _, team := range teamUsers {
		if project, ok := projects[team.Team.Name]; ok {
			st := &model.ServiceTeam{
				ServiceTeamID:   team.Team.ID,
				ServiceID:       1, // TODO : Hardcoded to FOSSA for now
				ServiceTeamName: &team.Team.Name,
				ProjectID:       project.ID,
				ProjectName:     &project.Name,
			}
			err := db.Where("service_team_id = ?", team.Team.ID).
				FirstOrCreate(st).Error
			if err != nil {
				msg := fmt.Sprintf("CreateServiceTeamsForUser: failed for team %d (%s): %v", team.Team.ID, team.Team.Name, err)
				log.Println(msg)
				errMessages = append(errMessages, msg)
				continue
			}
			teams = append(teams, st)
		} else {
			return nil, fmt.Errorf("CreateServiceTeamsForUser: ERROR %s is NOT A registered project", team.Team.Name)
		}
	}

	if len(teams) == 0 {
		return nil, fmt.Errorf("CreateServiceTeamsForUser: no valid teams created")
	}

	if len(errMessages) > 0 {
		return teams, fmt.Errorf("CreateServiceTeamsForUser had partial errors:\n%s", strings.Join(errMessages, "\n"))
	}

	return teams, nil
}

func MapFossaUserCollaborator(db *gorm.DB, email string, github string, user fossa.User) *model.Collaborator {
	c := model.Collaborator{
		Model:         gorm.Model{},
		Name:          user.FullName,
		Email:         user.Email,
		GitHubEmail:   &github,
		GitHubAccount: user.GitHub.Name,
		LastLogin:     user.LastVisit,
		RegisteredAt:  user.CreatedAt,
	}
	// TODO Fill out the Projects field

	if github != "" {
		if err := db.
			Where("LOWER(git_hub_account) = ?", strings.ToLower(github)).
			FirstOrCreate(&c).Error; err == nil {
			return &c
		} else {
			return nil
		}
	}

	if err := db.
		Where("LOWER(email) = ?", strings.ToLower(email)).
		FirstOrCreate(&c).Error; err == nil {
		return &c
	} else {
		log.Printf("mapFossaUserCollaborator: error creating collaborator: %s, %+v %v", email, c, err)
		return nil
	}
}

// MapFossaUserToMaintainer attempts to match a FOSSA user to a registered Maintainer.
// returns a *model.maintainer if found, nil if not found
func MapFossaUserToMaintainer(db *gorm.DB, email string, github string) *model.Maintainer {
	var m model.Maintainer

	if github != "" {
		if err := db.
			Where("LOWER(git_hub_account) = ?", strings.ToLower(github)).
			First(&m).Error; err == nil {
			return &m
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
	}

	if err := db.
		Where("LOWER(email) = ? OR LOWER(git_hub_email) = ?", strings.ToLower(email), strings.ToLower(email)).
		First(&m).Error; err == nil {
		return &m
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}

	// No match in the registered maintainers on the maintainer's table
	return nil
}

func LinkServiceUserToTeam(
	db *gorm.DB,
	su *model.ServiceUser,
	sTeams []*model.ServiceTeam,
	maintainer *model.Maintainer,
	collaborator *model.Collaborator,
) error {
	if su == nil {
		return fmt.Errorf("LinkServiceUserToTeam: service user is nil")
	}
	if len(sTeams) == 0 {
		return fmt.Errorf("LinkServiceUserToTeam: no service teams provided for user %d", su.ServiceUserID)
	}
	if maintainer != nil && collaborator != nil {
		return fmt.Errorf("LinkServiceUserToTeam: cannot link both a maintainer and a collaborator for user %d", su.ServiceUserID)
	}

	var linkErrors []string

	for _, st := range sTeams {
		if st == nil {
			continue
		}

		serviceUserTeams := model.ServiceUserTeams{
			ServiceID:     1, // TODO Remove Magic Number
			ServiceUserID: su.ServiceUserID,
		}

		err := db.Where("service_id = ? AND service_team_id = ? AND service_user_id = ?",
			su.ServiceID, st.ID, su.ServiceUserID).
			FirstOrCreate(&serviceUserTeams).Error

		if err == nil {
			// Update if necessary
			updated := false
			if maintainer != nil && serviceUserTeams.MaintainerID == nil {
				serviceUserTeams.MaintainerID = &maintainer.ID
				serviceUserTeams.CollaboratorID = nil
				updated = true
			}
			if collaborator != nil && serviceUserTeams.CollaboratorID == nil {
				serviceUserTeams.CollaboratorID = &collaborator.ID
				serviceUserTeams.MaintainerID = nil
				updated = true
			}
			if updated {
				if err := db.Save(&serviceUserTeams).Error; err != nil {
					linkErrors = append(linkErrors,
						fmt.Sprintf("ERR, db.save failed to update the serviceUserTeams link for user %d to team %d: %v",
							su.ServiceUserID, st.ID, err))
				} else {
					log.Printf("INFO, LinkServiceUserToTeam: %s linked as a collaborator on %s \n", su.ServiceEmail, *st.ServiceTeamName)
				}
			}
			continue
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("ERROR, LinkServiceUserToTeam, gorm.ErrRecordNotFound for %s to FOSSA team %s\n",
				su.ServiceEmail,
				*st.ServiceTeamName)
			linkErrors = append(linkErrors,
				fmt.Sprintf("lookup failure for link (user %d, team %d): %v", su.ServiceUserID, st.ID, err))
			continue
		}

		// No link exists, create new
		newLink := model.ServiceUserTeams{
			ServiceID:     su.ServiceID,
			ServiceUserID: su.ServiceUserID,
			ServiceTeamID: st.ID,
		}
		if maintainer != nil {
			newLink.MaintainerID = &maintainer.ID
		} else if collaborator != nil {
			newLink.CollaboratorID = &collaborator.ID
		}

		if err := db.Create(&newLink).Error; err != nil {
			linkErrors = append(linkErrors,
				fmt.Sprintf("failed to create link for user %d to team %d: %v", su.ServiceUserID, st.ID, err))
		}
	}

	if len(linkErrors) > 0 {
		return fmt.Errorf("LinkServiceUserToTeam: %d linking errors:\n%s", len(linkErrors), strings.Join(linkErrors, "\n"))
	}

	return nil
}

func FirstOrCreateServiceUser(db *gorm.DB, user fossa.User) (*model.ServiceUser, error) {
	var fossaService = model.Service{Model: gorm.Model{ID: 1}}

	var su model.ServiceUser

	lookup := model.ServiceUser{
		ServiceID:     fossaService.ID,
		ServiceUserID: user.ID,
	}

	// Fields to populate if the record is not found
	create := model.ServiceUser{
		ServiceGitHubName: user.GitHub.Name,
		ServiceEmail:      user.Email,
	}

	// find a service user with fields in lookup, and if not found, create it with these values
	if err := db.Where(&lookup).Attrs(&create).FirstOrCreate(&su).Error; err != nil {
		return nil, fmt.Errorf("loadFossa FirstOrCreateServiceUser failed for %v, err : %w", lookup, err)
	}

	return &su, nil
}

func MapFossaUserToMaintainerOrCollaborator(db *gorm.DB, user fossa.User) (model.Maintainer, model.Collaborator, error) {
	var m model.Maintainer
	var c model.Collaborator

	// Do we have a maintainer that matches this fossa User?
	if err := db.Where("LOWER(email) = ?", strings.ToLower(user.Email)).
		First(&m).Error; err == nil {
		return m, c, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return m, c, fmt.Errorf("query error during email lookup: %w", err)
	}

	// Do we have the Maintainer that has a GitHub handle match? (if present in FOSSA)
	if *user.GitHub.Name != "" {
		if err := db.Where("LOWER(git_hub_account) = ?", strings.ToLower(*user.GitHub.Name)).
			First(&m).Error; err == nil {
			return m, c, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			// Create a Collaborator record
			c = model.Collaborator{
				Model:         gorm.Model{},
				Name:          user.FullName,
				Email:         user.Email,
				GitHubAccount: user.GitHub.Name,
				LastLogin:     user.LastVisit,
				Projects:      nil,
				RegisteredAt:  user.Joined,
			}
			if err := db.FirstOrCreate(&c).Error; err == nil {
				return m, c, nil
			}

			return m, c, fmt.Errorf("query error during GitHub handle lookup for %s : %w", *user.GitHub.Name, err)
		}
	}

	fmt.Printf("fossa user %s, did not match an existing maintainer.\n FOSSA Teams: ", user.Email)
	// Let's make a Collaborator for the Project

	for _, teamUser := range user.TeamUsers {
		fmt.Printf("%s, ", teamUser.Team.Name)
	}
	if m.ID == 0 {
		return model.Maintainer{}, model.Collaborator{}, nil
	}
	return m, c, nil
}

func FetchFossaData(token string) ([]fossa.User, []fossa.Team, interface{}) {
	fossaClient := fossa.NewClient(token)

	users, err := fossaClient.FetchUsers()
	if err != nil {
		return nil, nil, err
	}

	teams, err := fossaClient.FetchTeams()
	if err != nil {
		return nil, nil, err
	}

	return users, teams, nil
}
func safeGitHubName(ghName *string) string {
	if ghName != nil {
		return *ghName
	}
	return ""
}
