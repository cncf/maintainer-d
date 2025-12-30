package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceReference expresses a loose reference to another resource. The UID is optional
// but helps controllers confirm identity when present.
type ResourceReference struct {
	// Name is the resource name within its workspace.
	Name string `json:"name"`
	// Workspace optionally identifies the kcp workspace that owns the target resource.
	Workspace string `json:"workspace,omitempty"`
	// UID is set by controllers that want to pin to a specific resource instance.
	UID types.UID `json:"uid,omitempty"`
}

// ProjectReference embeds additional project specific metadata for status views.
type ProjectReference struct {
	ResourceReference `json:",inline"`
	// Roles reflects the project roles this subject currently holds.
	Roles []string `json:"roles,omitempty"`
}

// MaintainerLifecycle enumerates lifecycle states for a maintainer.
type MaintainerLifecycle string

const (
	// MaintainerActive represents a maintainer who is actively serving.
	MaintainerActive MaintainerLifecycle = "Active"
	// MaintainerEmeritus represents a maintainer who has stepped back but retains emeritus status.
	MaintainerEmeritus MaintainerLifecycle = "Emeritus"
	// MaintainerRetired represents a maintainer who is no longer participating.
	MaintainerRetired MaintainerLifecycle = "Retired"
)

// ProjectMaturity enumerates the CNCF maturity stages.
type ProjectMaturity string

const (
	MaturitySandbox    ProjectMaturity = "Sandbox"
	MaturityIncubating ProjectMaturity = "Incubating"
	MaturityGraduated  ProjectMaturity = "Graduated"
	MaturityArchived   ProjectMaturity = "Archived"
)

// MaintainerSpec captures desired maintainer attributes.
type MaintainerSpec struct {
	DisplayName   string              `json:"displayName"`
	PrimaryEmail  string              `json:"primaryEmail,omitempty"`
	GitHubAccount string              `json:"gitHubAccount,omitempty"`
	GitHubEmail   string              `json:"gitHubEmail,omitempty"`
	Status        MaintainerLifecycle `json:"status"`
	CompanyRef    *ResourceReference  `json:"companyRef,omitempty"`
	RegisteredAt  *metav1.Time        `json:"registeredAt,omitempty"`
	ExternalIDs   map[string]string   `json:"externalIDs,omitempty"`
}

// MaintainerStatus surfaces derived information gathered by controllers.
type MaintainerStatus struct {
	ProjectMemberships []ProjectReference `json:"projectMemberships,omitempty"`
	ImportWarnings     []string           `json:"importWarnings,omitempty"`
	LastSynced         *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=maintainers,scope=Namespaced,shortName=maint,categories=maintainerd
// +kubebuilder:subresource:status

// Maintainer represents an individual with project responsibilities.
type Maintainer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MaintainerSpec   `json:"spec,omitempty"`
	Status MaintainerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MaintainerList is a list of Maintainer resources.
type MaintainerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Maintainer `json:"items"`
}

// StaffMemberSpec captures desired staff member attributes.
type StaffMemberSpec struct {
	DisplayName   string             `json:"displayName"`
	PrimaryEmail  string             `json:"primaryEmail,omitempty"`
	GitHubAccount string             `json:"gitHubAccount,omitempty"`
	GitHubEmail   string             `json:"gitHubEmail,omitempty"`
	FoundationRef *ResourceReference `json:"foundationRef,omitempty"`
	RegisteredAt  *metav1.Time       `json:"registeredAt,omitempty"`
	ExternalIDs   map[string]string  `json:"externalIDs,omitempty"`
}

// StaffMemberStatus surfaces derived information gathered by controllers.
type StaffMemberStatus struct {
	LastSynced *metav1.Time `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=staffmembers,scope=Namespaced,shortName=staff,categories=maintainerd
// +kubebuilder:subresource:status

// StaffMember represents a foundation staff member.
type StaffMember struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StaffMemberSpec   `json:"spec,omitempty"`
	Status StaffMemberStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StaffMemberList is a list of StaffMember resources.
type StaffMemberList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StaffMember `json:"items"`
}

// CollaboratorSpec captures collaborator specific attributes.
type CollaboratorSpec struct {
	DisplayName   string              `json:"displayName"`
	PrimaryEmail  string              `json:"primaryEmail,omitempty"`
	GitHubAccount string              `json:"gitHubAccount,omitempty"`
	GitHubEmail   string              `json:"gitHubEmail,omitempty"`
	LastLogin     *metav1.Time        `json:"lastLogin,omitempty"`
	RegisteredAt  *metav1.Time        `json:"registeredAt,omitempty"`
	Projects      []ResourceReference `json:"projects,omitempty"`
	ExternalIDs   map[string]string   `json:"externalIDs,omitempty"`
}

// CollaboratorStatus reports derived collaborator information.
type CollaboratorStatus struct {
	ObservedProjects []ProjectReference `json:"observedProjects,omitempty"`
	LastSynced       *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=collaborators,scope=Namespaced,shortName=collab,categories=maintainerd
// +kubebuilder:subresource:status

// Collaborator represents a contributor with limited responsibilities.
type Collaborator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CollaboratorSpec   `json:"spec,omitempty"`
	Status CollaboratorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CollaboratorList is a list of Collaborator resources.
type CollaboratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Collaborator `json:"items"`
}

// ProjectSpec captures desired project configuration.
type ProjectSpec struct {
	DisplayName       string              `json:"displayName"`
	Maturity          ProjectMaturity     `json:"maturity,omitempty"`
	ParentProjectRef  *ResourceReference  `json:"parentProjectRef,omitempty"`
	MaintainerLeadRef *ResourceReference  `json:"maintainerLeadRef,omitempty"`
	OnboardingIssue   string              `json:"onboardingIssue,omitempty"`
	MailingList       string              `json:"mailingList,omitempty"`
	FoundationRef     *ResourceReference  `json:"foundationRef,omitempty"`
	MaintainerRefs    []ResourceReference `json:"maintainerRefs,omitempty"`
	CollaboratorRefs  []ResourceReference `json:"collaboratorRefs,omitempty"`
	ServiceRefs       []ResourceReference `json:"serviceRefs,omitempty"`
	Tags              map[string]string   `json:"tags,omitempty"`
}

// ProjectStatus reports reconciliation signals for a project.
type ProjectStatus struct {
	MaintainerCount   int                `json:"maintainerCount,omitempty"`
	CollaboratorCount int                `json:"collaboratorCount,omitempty"`
	ServiceCount      int                `json:"serviceCount,omitempty"`
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
	LastSynced        *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=projects,scope=Namespaced,shortName=proj,categories=maintainerd
// +kubebuilder:subresource:status

// Project represents a CNCF project or subproject.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList is a list of Project resources.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

// CompanySpec describes a company that employs maintainers.
type CompanySpec struct {
	DisplayName string            `json:"displayName"`
	Website     string            `json:"website,omitempty"`
	Notes       string            `json:"notes,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// CompanyStatus provides aggregated company metrics.
type CompanyStatus struct {
	MaintainerCount   int                `json:"maintainerCount,omitempty"`
	CollaboratorCount int                `json:"collaboratorCount,omitempty"`
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=companies,scope=Namespaced,shortName=comp,categories=maintainerd
// +kubebuilder:subresource:status

// Company represents a legal entity affiliation for maintainers.
type Company struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CompanySpec   `json:"spec,omitempty"`
	Status CompanyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CompanyList is a list of Company resources.
type CompanyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Company `json:"items"`
}

// ServiceSpec describes a service that interfaces with CNCF projects.
type ServiceSpec struct {
	DisplayName string            `json:"displayName"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// ServiceStatus tracks derived associations for a service.
type ServiceStatus struct {
	ProjectRefs []ResourceReference `json:"projectRefs,omitempty"`
	TeamRefs    []ResourceReference `json:"teamRefs,omitempty"`
	Conditions  []metav1.Condition  `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=services,scope=Namespaced,shortName=svc,categories=maintainerd
// +kubebuilder:subresource:status

// Service captures a service integration such as FOSSA, GitHub, or Netlify.
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSpec   `json:"spec,omitempty"`
	Status ServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceList is a list of Service resources.
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Service `json:"items"`
}

// ProjectMembershipSpec models the relationship between a maintainer and a project.
type ProjectMembershipSpec struct {
	ProjectRef    ResourceReference `json:"projectRef"`
	MaintainerRef ResourceReference `json:"maintainerRef"`
	Roles         []string          `json:"roles,omitempty"`
	JoinedAt      *metav1.Time      `json:"joinedAt,omitempty"`
	Notes         string            `json:"notes,omitempty"`
}

// ProjectMembershipStatus surfaces reconciliation metadata about a membership.
type ProjectMembershipStatus struct {
	LastVerified *metav1.Time       `json:"lastVerified,omitempty"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=projectmemberships,scope=Namespaced,shortName=prjmem,categories=maintainerd
// +kubebuilder:subresource:status

// ProjectMembership establishes membership between a maintainer and a project.
type ProjectMembership struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectMembershipSpec   `json:"spec,omitempty"`
	Status ProjectMembershipStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectMembershipList is a list of ProjectMembership resources.
type ProjectMembershipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProjectMembership `json:"items"`
}

// ServiceTeamSpec captures the join between a service and project.
type ServiceTeamSpec struct {
	ServiceRef  ResourceReference `json:"serviceRef"`
	ProjectRef  ResourceReference `json:"projectRef"`
	RemoteID    string            `json:"remoteID,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	ProjectName string            `json:"projectName,omitempty"`
}

// ServiceTeamStatus tracks membership reconciliation state.
type ServiceTeamStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	LastSynced *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=serviceteams,scope=Namespaced,shortName=svcteam,categories=maintainerd
// +kubebuilder:subresource:status

// ServiceTeam represents a remote service team associated with a project.
type ServiceTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceTeamSpec   `json:"spec,omitempty"`
	Status ServiceTeamStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceTeamList is a list of ServiceTeam resources.
type ServiceTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceTeam `json:"items"`
}

// ServiceUserSpec captures the relationship between a person and an external service.
type ServiceUserSpec struct {
	ServiceRef      ResourceReference  `json:"serviceRef"`
	RemoteID        string             `json:"remoteID,omitempty"`
	Email           string             `json:"email"`
	Reference       string             `json:"reference,omitempty"`
	GitHubUser      string             `json:"gitHubUser,omitempty"`
	MaintainerRef   *ResourceReference `json:"maintainerRef,omitempty"`
	CollaboratorRef *ResourceReference `json:"collaboratorRef,omitempty"`
}

// ServiceUserStatus reports derived controller state for a service user.
type ServiceUserStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	LastSynced *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=serviceusers,scope=Namespaced,shortName=svcusr,categories=maintainerd
// +kubebuilder:subresource:status

// ServiceUser models an account on an external service used by maintainers.
type ServiceUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceUserSpec   `json:"spec,omitempty"`
	Status ServiceUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceUserList is a list of ServiceUser resources.
type ServiceUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceUser `json:"items"`
}

// ServiceUserTeamSpec connects a service user with a service team and optional people.
type ServiceUserTeamSpec struct {
	ServiceUserRef  ResourceReference  `json:"serviceUserRef"`
	ServiceTeamRef  ResourceReference  `json:"serviceTeamRef"`
	MaintainerRef   *ResourceReference `json:"maintainerRef,omitempty"`
	CollaboratorRef *ResourceReference `json:"collaboratorRef,omitempty"`
}

// ServiceUserTeamStatus captures controller reconciliation data.
type ServiceUserTeamStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	LastSynced *metav1.Time       `json:"lastSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=serviceuserteams,scope=Namespaced,shortName=svcutm,categories=maintainerd
// +kubebuilder:subresource:status

// ServiceUserTeam associates a service user with a service team membership.
type ServiceUserTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceUserTeamSpec   `json:"spec,omitempty"`
	Status ServiceUserTeamStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// ServiceUserTeamList is a list of ServiceUserTeam resources.
type ServiceUserTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceUserTeam `json:"items"`
}

// OnboardingTaskSpec describes a project onboarding task pulled from an external tracker.
type OnboardingTaskSpec struct {
	ProjectRef  ResourceReference `json:"projectRef"`
	Name        string            `json:"name"`
	Owner       string            `json:"owner,omitempty"`
	Number      int               `json:"number"`
	IssueURL    string            `json:"issueURL"`
	Completed   bool              `json:"completed"`
	CollectedAt metav1.Time       `json:"collectedAt"`
}

// OnboardingTaskStatus captures derived metadata for the task.
type OnboardingTaskStatus struct {
	LastSynced *metav1.Time       `json:"lastSynced,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=onboardingtasks,scope=Namespaced,shortName=onbtask,categories=maintainerd
// +kubebuilder:subresource:status

// OnboardingTask tracks onboarding progress for CNCF projects.
type OnboardingTask struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OnboardingTaskSpec   `json:"spec,omitempty"`
	Status OnboardingTaskStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OnboardingTaskList is a list of OnboardingTask resources.
type OnboardingTaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OnboardingTask `json:"items"`
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		SchemeGroupVersion,
		&Maintainer{}, &MaintainerList{},
		&StaffMember{}, &StaffMemberList{},
		&Collaborator{}, &CollaboratorList{},
		&Project{}, &ProjectList{},
		&Company{}, &CompanyList{},
		&Service{}, &ServiceList{},
		&ProjectMembership{}, &ProjectMembershipList{},
		&ServiceTeam{}, &ServiceTeamList{},
		&ServiceUser{}, &ServiceUserList{},
		&ServiceUserTeam{}, &ServiceUserTeamList{},
		&OnboardingTask{}, &OnboardingTaskList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
