package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	apis "maintainerd/apis/maintainers/v1alpha1"
	"maintainerd/db"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// In-cluster defaults: PVC mounted at /data in the CronJob.
	defaultDBPath    = "/data/maintainers.db"
	defaultNamespace = "maintainerd"
)

func main() {
	ctx := context.Background()

	dbConn, err := openDB(defaultDBPath)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	store := db.NewSQLStore(dbConn)

	k8sClient, err := newClient()
	if err != nil {
		log.Fatalf("failed to create k8s client: %v", err)
	}

	if err := syncAll(ctx, store, k8sClient, defaultNamespace); err != nil {
		log.Fatalf("sync failed: %v", err)
	}

	log.Println("sync completed successfully")
}

func openDB(path string) (*gorm.DB, error) {
	dbConn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return dbConn, nil
}

func newClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add client-go scheme: %w", err)
	}
	if err := apis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add maintainer-d scheme: %w", err)
	}

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("building kube config: %w", err)
	}

	return client.New(restCfg, client.Options{Scheme: scheme})
}

func syncAll(ctx context.Context, store *db.SQLStore, c client.Client, ns string) error {
	if err := syncCompanies(ctx, store, c, ns); err != nil {
		return fmt.Errorf("companies: %w", err)
	}
	if err := syncMaintainers(ctx, store, c, ns); err != nil {
		return fmt.Errorf("maintainers: %w", err)
	}
	if err := syncProjects(ctx, store, c, ns); err != nil {
		return fmt.Errorf("projects: %w", err)
	}
	if err := syncMemberships(ctx, store, c, ns); err != nil {
		return fmt.Errorf("projectmemberships: %w", err)
	}
	return nil
}

func syncCompanies(ctx context.Context, store *db.SQLStore, c client.Client, ns string) error {
	companies, err := store.ListCompanies()
	if err != nil {
		return err
	}
	for _, comp := range companies {
		obj := &apis.Company{}
		name := sanitizeName(comp.Name)
		key := client.ObjectKey{Name: name, Namespace: ns}
		err := c.Get(ctx, key, obj)
		if errors.IsNotFound(err) {
			obj = &apis.Company{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec:       apis.CompanySpec{DisplayName: comp.Name},
			}
			if err := c.Create(ctx, obj); err != nil {
				return fmt.Errorf("create company %s: %w", name, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if obj.Spec.DisplayName != comp.Name {
			obj.Spec.DisplayName = comp.Name
			if err := c.Update(ctx, obj); err != nil {
				return fmt.Errorf("update company %s: %w", name, err)
			}
		}
	}
	return nil
}

func syncMaintainers(ctx context.Context, store *db.SQLStore, c client.Client, ns string) error {
	mByEmail, err := store.GetMaintainerMapByEmail()
	if err != nil {
		return err
	}
	for _, m := range mByEmail {
		name := sanitizeName(m.Email)
		obj := &apis.Maintainer{}
		key := client.ObjectKey{Name: name, Namespace: ns}
		err := c.Get(ctx, key, obj)
		status := apis.MaintainerLifecycle(m.MaintainerStatus)
		if status == "" {
			status = apis.MaintainerActive
		}
		var registeredAt *metav1.Time
		if m.RegisteredAt != nil {
			t := metav1.NewTime(*m.RegisteredAt)
			registeredAt = &t
		}
		spec := apis.MaintainerSpec{
			DisplayName:   m.Name,
			PrimaryEmail:  m.Email,
			GitHubAccount: m.GitHubAccount,
			GitHubEmail:   m.GitHubEmail,
			Status:        status,
			RegisteredAt:  registeredAt,
		}
		if m.CompanyID != nil && m.Company.Name != "" {
			spec.CompanyRef = &apis.ResourceReference{Name: sanitizeName(m.Company.Name)}
		}
		if errors.IsNotFound(err) {
			obj = &apis.Maintainer{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec:       spec,
			}
			if m.CompanyID != nil && m.Company.Name != "" {
				obj.Spec.CompanyRef = &apis.ResourceReference{Name: sanitizeName(m.Company.Name)}
			}
			if err := c.Create(ctx, obj); err != nil {
				return fmt.Errorf("create maintainer %s: %w", name, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !maintainerSpecEqual(obj.Spec, spec) {
			obj.Spec = spec
			if err := c.Update(ctx, obj); err != nil {
				return fmt.Errorf("update maintainer %s: %w", name, err)
			}
		}
	}
	return nil
}

func syncProjects(ctx context.Context, store *db.SQLStore, c client.Client, ns string) error {
	projectsByName, err := store.GetProjectMapByName()
	if err != nil {
		return err
	}
	parentNameByID := make(map[uint]string, len(projectsByName))
	for _, p := range projectsByName {
		parentNameByID[p.ID] = p.Name
	}
	for _, p := range projectsByName {
		name := sanitizeName(p.Name)
		obj := &apis.Project{}
		key := client.ObjectKey{Name: name, Namespace: ns}
		spec := apis.ProjectSpec{
			DisplayName:    p.Name,
			Maturity:       apis.ProjectMaturity(p.Maturity),
			MaintainerRefs: make([]apis.ResourceReference, 0, len(p.Maintainers)),
		}
		if p.OnboardingIssue != nil {
			spec.OnboardingIssue = *p.OnboardingIssue
		}
		for _, m := range p.Maintainers {
			spec.MaintainerRefs = append(spec.MaintainerRefs, apis.ResourceReference{Name: sanitizeName(m.Email)})
		}
		if p.MailingList != nil {
			spec.MailingList = *p.MailingList
		}
		if p.MaintainerRef != "" {
			spec.MaintainerLeadRef = &apis.ResourceReference{Name: sanitizeName(p.MaintainerRef)}
		}
		if p.ParentProjectID != nil {
			if parentName, ok := parentNameByID[*p.ParentProjectID]; ok {
				spec.ParentProjectRef = &apis.ResourceReference{Name: sanitizeName(parentName)}
			}
		}
		err := c.Get(ctx, key, obj)
		if errors.IsNotFound(err) {
			obj = &apis.Project{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec:       spec,
			}
			if err := c.Create(ctx, obj); err != nil {
				return fmt.Errorf("create project %s: %w", name, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !projectSpecEqual(obj.Spec, spec) {
			obj.Spec = spec
			if err := c.Update(ctx, obj); err != nil {
				return fmt.Errorf("update project %s: %w", name, err)
			}
		}
	}
	return nil
}

func syncMemberships(ctx context.Context, store *db.SQLStore, c client.Client, ns string) error {
	projectsByName, err := store.GetProjectMapByName()
	if err != nil {
		return err
	}
	for _, p := range projectsByName {
		for _, m := range p.Maintainers {
			name := sanitizeName(fmt.Sprintf("%s-%s", p.Name, m.Email))
			obj := &apis.ProjectMembership{}
			key := client.ObjectKey{Name: name, Namespace: ns}
			spec := apis.ProjectMembershipSpec{
				ProjectRef:    apis.ResourceReference{Name: sanitizeName(p.Name)},
				MaintainerRef: apis.ResourceReference{Name: sanitizeName(m.Email)},
			}
			err := c.Get(ctx, key, obj)
			if errors.IsNotFound(err) {
				obj = &apis.ProjectMembership{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec:       spec,
				}
				if err := c.Create(ctx, obj); err != nil {
					return fmt.Errorf("create membership %s: %w", name, err)
				}
				continue
			}
			if err != nil {
				return err
			}
			if obj.Spec.ProjectRef.Name != spec.ProjectRef.Name || obj.Spec.MaintainerRef.Name != spec.MaintainerRef.Name {
				obj.Spec = spec
				if err := c.Update(ctx, obj); err != nil {
					return fmt.Errorf("update membership %s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func sanitizeName(s string) string {
	if s == "" {
		return "unnamed"
	}
	// Align with DNS-1123 label requirements used by K8s object names.
	s = strings.ToLower(strings.TrimSpace(s))
	s = dns1123InvalidChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "unnamed"
	}
	if len(s) > 63 {
		s = s[:63]
		s = strings.Trim(s, "-.")
	}
	if s == "" {
		return "unnamed"
	}
	return s
}

func projectSpecEqual(a, b apis.ProjectSpec) bool {
	if a.DisplayName != b.DisplayName || a.Maturity != b.Maturity || a.MailingList != b.MailingList || a.OnboardingIssue != b.OnboardingIssue {
		return false
	}
	if (a.ParentProjectRef == nil) != (b.ParentProjectRef == nil) {
		return false
	}
	if a.ParentProjectRef != nil && a.ParentProjectRef.Name != b.ParentProjectRef.Name {
		return false
	}
	if (a.MaintainerLeadRef == nil) != (b.MaintainerLeadRef == nil) {
		return false
	}
	if a.MaintainerLeadRef != nil && a.MaintainerLeadRef.Name != b.MaintainerLeadRef.Name {
		return false
	}
	if len(a.MaintainerRefs) != len(b.MaintainerRefs) {
		return false
	}
	// compare sets by name
	ma := make(map[string]struct{}, len(a.MaintainerRefs))
	for _, r := range a.MaintainerRefs {
		ma[r.Name] = struct{}{}
	}
	for _, r := range b.MaintainerRefs {
		if _, ok := ma[r.Name]; !ok {
			return false
		}
	}
	return true
}

func maintainerSpecEqual(a, b apis.MaintainerSpec) bool {
	if a.DisplayName != b.DisplayName ||
		a.PrimaryEmail != b.PrimaryEmail ||
		a.GitHubAccount != b.GitHubAccount ||
		a.GitHubEmail != b.GitHubEmail ||
		a.Status != b.Status ||
		!timePtrEqual(a.RegisteredAt, b.RegisteredAt) {
		return false
	}
	if (a.CompanyRef == nil) != (b.CompanyRef == nil) {
		return false
	}
	if a.CompanyRef != nil && a.CompanyRef.Name != b.CompanyRef.Name {
		return false
	}
	return true
}

func timePtrEqual(a, b *metav1.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Time.Equal(b.Time)
	}
}

var dns1123InvalidChars = regexp.MustCompile(`[^a-z0-9-.]+`)
