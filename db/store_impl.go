package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maintainerd/model"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type SQLStore struct {
	db *gorm.DB
}

func NewSQLStore(db *gorm.DB) *SQLStore {
	return &SQLStore{db: db}
}

// Ping verifies the underlying database connection is healthy.
func (s *SQLStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql store is not initialized")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// getServiceByName returns a &Service the service identified by name
func (s *SQLStore) getServiceByName(name string) (*model.Service, error) {
	var svc model.Service
	err := s.db.Where("name = ?", name).First(&svc).Error
	return &svc, err
}
func (s *SQLStore) GetProjectsUsingService(serviceID uint) ([]model.Project, error) {
	var projects []model.Project
	err := s.db.
		Joins("JOIN service_teams st ON st.project_id = projects.id").
		Where("st.service_id = ?", serviceID).
		Preload("Maintainers.Company").
		Find(&projects).Error
	return projects, err
}

func (s *SQLStore) GetMaintainersByProject(projectID uint) ([]model.Maintainer, error) {
	var project model.Project
	err := s.db.
		Preload("Maintainers.Company").
		First(&project, projectID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	return project.Maintainers, nil

}

func (s *SQLStore) GetServiceTeamByProject(projectID, serviceID uint) (*model.ServiceTeam, error) {
	var st model.ServiceTeam
	err := s.db.
		Where("project_id = ? AND service_id = ?", projectID, serviceID).
		First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &st, err
}

// GetMaintainerMapByEmail returns a map of Maintainers keyed by email address
func (s *SQLStore) GetMaintainerMapByEmail() (map[string]model.Maintainer, error) {
	var maintainers []model.Maintainer
	err := s.db.Preload("Company").Find(&maintainers).Error
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.Maintainer)
	for _, maintainer := range maintainers {
		m[maintainer.Email] = maintainer
	}
	return m, nil
}

// GetMaintainerMapByGitHubAccount returns a map of Maintainers keyed by GitHub Account
func (s *SQLStore) GetMaintainerMapByGitHubAccount() (map[string]model.Maintainer, error) {
	var maintainers []model.Maintainer
	err := s.db.Preload("Company").Find(&maintainers).Error
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.Maintainer)
	for _, maintainer := range maintainers {
		m[maintainer.GitHubAccount] = maintainer
	}
	return m, nil
}

// GetProjectServiceTeamMap returns a map of projectID to ServiceTeams
// for every Project that uses the service identified by serviceId
func (s *SQLStore) GetProjectServiceTeamMap(serviceName string) (map[uint]*model.ServiceTeam, error) {
	var serviceTeams []model.ServiceTeam
	service, err := s.getServiceByName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service, %s, by name: %v", serviceName, err)
	}
	// Preload the many-to-many relationship
	err = s.db.
		Where("service_id = ? ", service.ID).
		Find(&serviceTeams).Error
	if err != nil {
		return nil, fmt.Errorf("querying ServiceTeam for service_id %d: %w", service.ID, err)
	}

	result := make(map[uint]*model.ServiceTeam, len(serviceTeams))

	for i := range serviceTeams {
		st := &serviceTeams[i]
		result[st.ProjectID] = st
	}

	return result, nil

}
func (s *SQLStore) GetProjectMapByName() (map[string]model.Project, error) {
	var projects []model.Project
	if err := s.db.
		Preload("Maintainers").
		Preload("Maintainers.Company").
		Find(&projects).Error; err != nil {
		return nil, err
	}

	projectsByName := make(map[string]model.Project)
	for _, p := range projects {
		projectsByName[p.Name] = p
	}
	return projectsByName, nil
}

func (s *SQLStore) LogAuditEvent(logger *zap.SugaredLogger, event model.AuditLog) {
	if event.Message == "" {
		event.Message = event.Action
	}

	err := s.db.WithContext(context.Background()).Create(&event).Error
	if err != nil {
		logger.Errorf("failed to write %v audit log: %v", event, err)
	}
}

// CreateServiceTeam creates or retrieves a service team entry in the database based on the provided project and service details.
// It accepts a project ID, project name, service ID, and service name as input and returns the service team or an error.
func (s *SQLStore) CreateServiceTeam(
	projectID uint, projectName string,
	serviceID int, serviceName string) (*model.ServiceTeam, error) {

	var errMessages []string

	st := &model.ServiceTeam{
		ServiceTeamID:   serviceID,
		ServiceID:       1, // TODO : Hardcoded to FOSSA for now
		ServiceTeamName: &serviceName,
		ProjectID:       projectID,
		ProjectName:     &projectName,
	}
	err := s.db.Where("service_team_id = ?", serviceID).FirstOrCreate(st).Error
	if err != nil {
		msg := fmt.Sprintf("CreateServiceTeamsForUser: failed for team %d (%s): %v", serviceID, serviceName, err)
		log.Println(msg)
		return nil, fmt.Errorf("CreateServiceTeamsForUser had partial errors:\n%s", strings.Join(errMessages, "\n"))
	}
	return st, nil
}

// ListCompanies returns all companies in the database.
func (s *SQLStore) ListCompanies() ([]model.Company, error) {
	var companies []model.Company
	if err := s.db.Find(&companies).Error; err != nil {
		return nil, err
	}
	return companies, nil
}

// ListStaffMembers returns all staff members in the database, including their foundations.
func (s *SQLStore) ListStaffMembers() ([]model.StaffMember, error) {
	var staffMembers []model.StaffMember
	if err := s.db.Preload("Foundation").Find(&staffMembers).Error; err != nil {
		return nil, err
	}
	return staffMembers, nil
}

// IsStaffGitHubAccount returns true if the GitHub account belongs to a staff member.
func (s *SQLStore) IsStaffGitHubAccount(githubAccount string) (bool, error) {
	if githubAccount == "" {
		return false, nil
	}
	var count int64
	err := s.db.
		Model(&model.StaffMember{}).
		Where("LOWER(git_hub_account) = ?", strings.ToLower(githubAccount)).
		Count(&count).Error
	return count > 0, err
}
