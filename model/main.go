package model

import (
	"database/sql/driver"
	"fmt"
	"net/url"
	"time"

	"gorm.io/gorm"
)

type MaintainerStatus string

const (
	ActiveMaintainer   MaintainerStatus = "Active"
	EmeritusMaintainer MaintainerStatus = "Emeritus"
	RetiredMaintainer  MaintainerStatus = "Retired"
)

// IsValid returns true id MaintainerStatus is known
func (s MaintainerStatus) IsValid() bool {
	switch s {
	case ActiveMaintainer, EmeritusMaintainer, RetiredMaintainer:
		return true
	}
	return false
}

func (s *MaintainerStatus) Scan(value interface{ any }) error {
	v, ok := value.(string)
	if !ok {
		return fmt.Errorf("cannot scan %T into MaintainerStatus", value)
	}
	*s = MaintainerStatus(v)
	return nil
}

func (s MaintainerStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid MaintainerStatus %q", s)
	}
	return string(s), nil
}

func (m *Maturity) Scan(value interface{ any }) error {
	v, ok := value.(string)
	if !ok {
		return fmt.Errorf("cannot scan %T into Maturity", value)
	}
	*m = Maturity(v)
	return nil
}

type Maturity string

const (
	Sandbox    Maturity = "Sandbox"
	Incubating Maturity = "Incubating"
	Graduated  Maturity = "Graduated"
	Archived   Maturity = "Archived"
)

func (m Maturity) Value() (driver.Value, error) {
	if !m.IsValid() {
		return nil, fmt.Errorf("invalid Maturity %q", m)
	}
	return string(m), nil
}

func (m Maturity) IsValid() bool {
	switch m {
	case Sandbox, Incubating, Graduated, Archived:
		return true
	}
	return false
}

// A Maintainer is a leader that can speak for a Project
//
// At registration, an email needs to be provided
// Optionally, a Maintainer
//
//		has a Company Affiliation
//	  	Fot kubernetes specifically, a maintainer or may not have voting rights on a Project,
//	    has a status of Active, Emeritus or Retired
type Maintainer struct {
	gorm.Model
	Name             string
	Email            string           `gorm:"size:254;default:EMAIL_MISSING"`
	GitHubAccount    string           `gorm:"size:100;default:GITHUB_MISSING"`
	GitHubEmail      string           `gorm:"size:100;default:GITHUB_MISSING"`
	MaintainerStatus MaintainerStatus `gorm:"type:text"`
	ImportWarnings   string
	Projects         []Project `gorm:"many2many:maintainer_projects;joinForeignKey:MaintainerID;joinReferences:ProjectID"`
	RegisteredAt     *time.Time
	CompanyID        *uint
	Company          Company
}
type Collaborator struct {
	gorm.Model
	Name          string
	Email         string    `gorm:"size:254;default:EMAIL_MISSING"`
	GitHubEmail   *string   `gorm:"size:254;default:GITHUB_EMAIL_MISSING"`
	GitHubAccount *string   `gorm:"size:100;default:GITHUB_MISSING"`
	Projects      []Project `gorm:"many2many:maintainer_projects;joinForeignKey:MaintainerID;joinReferences:ProjectID"`
	LastLogin     time.Time
	RegisteredAt  time.Time
}
type Project struct {
	gorm.Model
	Name            string `gorm:"uniqueIndex,not null;check:name <> ''"`
	ParentProjectID *uint  `gorm:"index"`
	Maturity        Maturity
	MaintainerRef   string
	OnboardingIssue *string
	MailingList     *string      `gorm:"size:254;default:MML_MISSING"`
	Maintainers     []Maintainer `gorm:"many2many:maintainer_projects;joinForeignKey:ProjectID;joinReferences:MaintainerID"`
	Services        []Service    `gorm:"many2many:service_projects;joinForeignKey:ProjectID;joinReferences:ServiceID"`
}

type MaintainerProject struct {
	MaintainerID uint       `gorm:"primaryKey;index"` // FK + index
	ProjectID    uint       `gorm:"primaryKey;index"` // FK + index
	JoinedAt     time.Time  `gorm:"autoCreateTime"`
	Maintainer   Maintainer `gorm:"foreignKey:MaintainerID;constraint:OnDelete:CASCADE"`
	Project      Project    `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

type Company struct {
	gorm.Model
	Name string `gorm:"uniqueIndex"`
}

// A Foundation represents an organization that employs Staff members working with
// CNCF projects (e.g., CNCF and LF).
type Foundation struct {
	gorm.Model
	Name string `gorm:"uniqueIndex"`
}

// A StaffMember is a CNCF/LF staff member who may perform operations across projects.
// Staff members are associated with a Foundation (not a Project).
type StaffMember struct {
	gorm.Model
	Name          string
	Email         string `gorm:"size:254;default:EMAIL_MISSING"`
	GitHubAccount string `gorm:"size:100;default:GITHUB_MISSING"`
	GitHubEmail   string `gorm:"size:254;default:GITHUB_EMAIL_MISSING"`
	RegisteredAt  *time.Time

	FoundationID *uint `gorm:"index"`
	Foundation   Foundation
}
type Service struct {
	gorm.Model
	Name        string `gorm:"uniqueIndex"`
	Description string
}

type ServiceUserTeams struct {
	gorm.Model

	ServiceID     uint `gorm:"index"` // This may be redundant — if already tracked via foreign keys below
	ServiceUserID int  `gorm:"index"` // foreign key part (ServiceUser.ServiceUserID)

	ServiceTeamID uint        `gorm:"index"` // FK to ServiceTeam
	ServiceTeam   ServiceTeam `gorm:"foreignKey:ServiceTeamID;constraint:OnDelete:CASCADE"`

	MaintainerID   *uint `gorm:"index"` // nullable FK to Maintainer
	CollaboratorID *uint `gorm:"index"` // nullable FK to Collaborator
}

type ServiceTeam struct {
	gorm.Model
	ProjectID       uint `gorm:"index"` // FK to project
	ServiceID       uint `gorm:"index"` // FK to service
	ServiceTeamID   int  // ID on the remote service (e.g., FOSSA team ID)
	ServiceTeamName *string
	ProjectName     *string // De-normalised for debugging purposes
}

type ServiceUser struct {
	gorm.Model
	ServiceID         uint   `gorm:"index"` // FK to Service
	ServiceUserID     int    `gorm:"index"` // ID on the remote service
	ServiceEmail      string `gorm:"size:254;default:EMAIL_MISSING"`
	ServiceRef        string `gorm:"size:512"`
	ServiceGitHubName *string
}

// A FoundationOfficer is a person who has elevated access to
// Services to carry out Maintainer Operations on behalf of the
// Foundation that governs projects.
type FoundationOfficer struct {
	gorm.Model
	Name          string
	Email         string `gorm:"size:254;default:EMAIL_MISSING"`
	GitHubAccount string `gorm:"size:100;default:GITHUB_MISSING"`
	RegisteredAt  *time.Time
	CompanyID     *uint
	// Services represent user identities on external services (e.g., FOSSA) that
	// this officer can operate as. This is a many-to-many relationship because a
	// service user could (in theory) be shared across officers.
	Services []ServiceUser `gorm:"many2many:foundation_officer_service_users;constraint:OnDelete:CASCADE"`
}

type ReconciliationResult struct {
	gorm.Model
	Service              Service
	ProjectID            *uint
	MissingMaintainerIDs []*uint
}

// ProjectInfo is an in-memory cache. TODO Review this
type ProjectInfo struct {
	Project     Project
	Maintainers []Maintainer
	Services    []Service
}

type AuditLog struct {
	gorm.Model
	ProjectID    uint   `gorm:"index"`
	MaintainerID *uint  `gorm:"index"`
	ServiceID    *uint  `gorm:"index"`
	Action       string `gorm:"index"` // e.g. "ADD_MEMBER", "REMOVE_MEMBER", "INVITE_SENT"
	Message      string // human-readable message, optional
	Metadata     string // optional JSON blob for advanced inspection
}

type OnboardingTask struct {
	Name        string    `json:"name"`
	Owner       string    `json:"owner"`
	Number      int       `json:"number"`
	Complete    bool      `json:"competed"`
	Issue       url.URL   `json:"issue"`
	CollectedAt time.Time `json:"collected_at"`
}
