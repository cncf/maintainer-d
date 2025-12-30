package db

import (
	"errors"

	"maintainerd/model"

	"go.uber.org/zap"
)

var ErrProjectNotFound = errors.New("project not found")

type Store interface {
	GetProjectsUsingService(serviceID uint) ([]model.Project, error)
	GetProjectMapByName() (map[string]model.Project, error)
	GetMaintainersByProject(projectID uint) ([]model.Maintainer, error)
	GetProjectServiceTeamMap(serviceName string) (map[uint]*model.ServiceTeam, error)
	GetMaintainerMapByEmail() (map[string]model.Maintainer, error)
	GetServiceTeamByProject(projectID uint, serviceID uint) (*model.ServiceTeam, error)
	LogAuditEvent(logger *zap.SugaredLogger, event model.AuditLog) error
	GetMaintainerMapByGitHubAccount() (map[string]model.Maintainer, error)
	CreateServiceTeamForUser(interface{ any }) (*model.ServiceTeam, error)
	ListCompanies() ([]model.Company, error)
	ListStaffMembers() ([]model.StaffMember, error)
}
