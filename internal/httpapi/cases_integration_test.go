//go:build integration

package httpapi_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/httpapi/casefile"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// TestHTTPCases runs every testdata/cases/*.yaml file: it seeds a database
// per file (or per request for isolate: true cases), sends each case's
// request once per actor, and checks status, headers, body subset and the
// unchanged tables. See package casefile for the format.
func TestHTTPCases(t *testing.T) {
	paths, err := filepath.Glob("testdata/cases/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no testdata/cases/*.yaml files found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := casefile.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			runCaseFile(t, f)
		})
	}
}

func runCaseFile(t *testing.T, f casefile.File) {
	var shared *caseWorld
	for _, c := range f.Cases {
		if !c.Isolate {
			shared = seedCaseWorld(t, f.Seed)
			break
		}
	}
	for _, c := range f.Cases {
		for _, actor := range c.As {
			t.Run(c.Name+"/"+actor, func(t *testing.T) {
				w := shared
				if c.Isolate {
					w = seedCaseWorld(t, f.Seed)
				}
				runCase(t, filepath.Base(f.Path), w, c, actor)
			})
		}
	}
}

// caseWorld is a seeded database, the handler over it and each actor's
// session token.
type caseWorld struct {
	env    *testkit.Env
	h      http.Handler
	ids    map[string]int64
	tokens map[string]string
}

// seedCaseWorld applies s in a fixed order (orgs, settings, contacts,
// users, properties, leases, projects, tasks, invoices, invites), each map in
// sorted key order so IDs are deterministic, then logs every user in.
func seedCaseWorld(t *testing.T, s casefile.Seed) *caseWorld {
	t.Helper()
	e := testkit.New(t)
	ids := map[string]int64{}

	orgs := map[string]core.Org{}
	for _, slug := range s.Orgs {
		orgs[slug] = e.Org(slug)
	}
	for _, slug := range sortedKeys(s.Settings) {
		st := s.Settings[slug]
		opts := []testkit.SettingsOption{testkit.GSTRegistered(st.GSTRegistered)}
		if st.PayOptions {
			opts = append(opts, testkit.PayOptions())
		}
		e.Settings(orgs[slug], opts...)
		if st.EmergencyPhone != "" {
			if _, err := e.Pool.Exec(e.Ctx, `UPDATE orgs SET emergency_phone = $1 WHERE id = $2`, st.EmergencyPhone, orgs[slug].ID); err != nil {
				t.Fatalf("settings.%s: %v", slug, err)
			}
		}
	}
	for _, name := range sortedKeys(s.Contacts) {
		c := s.Contacts[name]
		var opts []testkit.ContactOption
		if c.Kind != "" {
			opts = append(opts, testkit.Kind(c.Kind))
		}
		if c.Name != "" {
			opts = append(opts, testkit.Name(c.Name))
		}
		if c.Email != "" {
			opts = append(opts, testkit.Email(c.Email))
		}
		ids[name] = e.Contact(orgs[c.Org], opts...).ID
	}
	users := map[string]testkit.User{}
	for _, name := range sortedKeys(s.Users) {
		u := s.Users[name]
		email := name + "@example.com"
		role := auth.Role(u.Role)
		if role == auth.Tenant || role == auth.Client {
			users[name] = e.PortalUser(email, orgs[u.Org], role, ids[u.Contact])
		} else {
			users[name] = e.User(email)
			e.Member(users[name], orgs[u.Org], role)
		}
		ids[name] = users[name].ID
	}
	for _, name := range sortedKeys(s.StrataSchemes) {
		sc := s.StrataSchemes[name]
		opts := []testkit.StrataSchemeOption{testkit.CTSNumber(sc.CTS)}
		if sc.Name != "" {
			opts = append(opts, testkit.SchemeName(sc.Name))
		}
		ids[name] = e.StrataScheme(orgs[sc.Org], opts...).ID
	}
	for _, name := range sortedKeys(s.Properties) {
		p := s.Properties[name]
		var opts []testkit.PropertyOption
		if p.Name != "" {
			opts = append(opts, testkit.PropertyName(p.Name))
		}
		if p.Commercial {
			opts = append(opts, testkit.Commercial())
		}
		ids[name] = e.Property(orgs[p.Org], opts...).ID
		if p.StrataScheme != "" {
			scheme := ids[p.StrataScheme]
			if err := property.SetPropertyStrataScheme(e.Ctx, e.Pool, orgs[p.Org].ID, ids[name], &scheme); err != nil {
				t.Fatalf("properties.%s: strata_scheme: %v", name, err)
			}
		}
	}
	leaseInvoices := map[string][]int64{}
	for _, name := range sortedKeys(s.Leases) {
		l := s.Leases[name]
		var opts []testkit.LeaseOption
		if l.Rent != 0 {
			opts = append(opts, testkit.Rent(core.Cents(l.Rent)))
		}
		for _, co := range l.CoTenants {
			opts = append(opts, testkit.CoTenant(ids[co]))
		}
		lease := e.Lease(orgs[l.Org], ids[l.Property], ids[l.Tenant], opts...)
		ids[name] = lease.ID
		if l.Terminated != "" {
			if _, err := e.Pool.Exec(e.Ctx, `UPDATE leases SET termination_date = $1 WHERE id = $2`, l.Terminated, lease.ID); err != nil {
				t.Fatalf("leases.%s: terminating: %v", name, err)
			}
		}
		if l.RentRun {
			leaseInvoices[name] = e.IssueRent(orgs[l.Org], lease)
		}
	}
	for _, name := range sortedKeys(s.Payments) {
		p := s.Payments[name]
		invoices := leaseInvoices[p.Lease]
		if p.Period >= len(invoices) {
			t.Fatalf("payments.%s: lease %q has %d rent invoices", name, p.Lease, len(invoices))
		}
		ids[name] = e.Payment(orgs[p.Org], invoices[p.Period], core.Cents(p.Amount), testkit.Date("2026-01-06"))
	}
	for _, name := range sortedKeys(s.Tickets) {
		tk := s.Tickets[name]
		lease := ids[tk.Lease]
		contact := ids[tk.RaisedBy]
		ticket := e.Ticket(orgs[tk.Org], property.NewTicket{LeaseID: &lease, ContactID: &contact, Title: tk.Title}, tk.Status, tk.Note)
		ids[name] = ticket.ID
		if tk.Supplier != "" {
			supplier := ids[tk.Supplier]
			if _, err := e.Pool.Exec(e.Ctx, `UPDATE maintenance_tickets SET supplier_contact_id = $1, status = 'assigned' WHERE id = $2`, supplier, ticket.ID); err != nil {
				t.Fatalf("tickets.%s: supplier: %v", name, err)
			}
		}
		if tk.Visit {
			start := time.Date(2030, 1, 7, 9, 0, 0, 0, time.UTC)
			v, err := property.BookVisit(e.Ctx, e.Pool, orgs[tk.Org].ID, ticket.ID, *ticket.RaisedByUserID, nil, start, start.Add(2*time.Hour))
			if err != nil {
				t.Fatalf("tickets.%s: visit: %v", name, err)
			}
			ids[name+"_visit"] = v.ID
		}
	}
	for _, name := range sortedKeys(s.Projects) {
		p := s.Projects[name]
		var opts []testkit.ProjectOption
		if p.Name != "" {
			opts = append(opts, testkit.ProjectName(p.Name))
		}
		if p.Status != "" {
			opts = append(opts, testkit.ProjectStatus(p.Status))
		}
		ids[name] = e.Project(orgs[p.Org], ids[p.Client], opts...).ID
	}
	for _, name := range sortedKeys(s.Tasks) {
		tk := s.Tasks[name]
		var opts []testkit.TaskOption
		if tk.Name != "" {
			opts = append(opts, testkit.TaskName(tk.Name))
		}
		if tk.Rate != 0 {
			opts = append(opts, testkit.TaskRate(core.Cents(tk.Rate)))
		}
		ids[name] = e.Task(orgs[tk.Org], ids[tk.Project], opts...).ID
	}
	for _, name := range sortedKeys(s.Invoices) {
		inv := s.Invoices[name]
		var opts []testkit.InvoiceOption
		if inv.Amount != 0 {
			opts = append(opts, testkit.Amount(inv.Amount))
		}
		if inv.Source != "" {
			opts = append(opts, testkit.Source(inv.Source))
		}
		if inv.Status == "issued" {
			opts = append(opts, testkit.Issued(time.Now()))
		}
		ids[name] = e.Invoice(orgs[inv.Org], ids[inv.Contact], opts...).ID
		if inv.Project != "" {
			if _, err := e.Pool.Exec(e.Ctx, `UPDATE invoice_lines SET project_id = $1 WHERE invoice_id = $2`, ids[inv.Project], ids[name]); err != nil {
				t.Fatalf("invoices.%s: project: %v", name, err)
			}
		}
	}
	store := storage.NewDisk(t.TempDir())
	seedClientPortal(t, e, s, orgs, ids, users, store)
	for _, name := range sortedKeys(s.Invites) {
		inv := s.Invites[name]
		var opts []testkit.InviteOption
		if inv.Expired {
			opts = append(opts, testkit.Expired())
		}
		ids[name] = e.Invite(orgs[inv.Org], inv.Email, auth.Role(inv.Role), inv.Token, opts...)
	}

	h := e.Handler(testkit.Modules(s.Modules), testkit.Store(store))
	tokens := map[string]string{casefile.Anonymous: ""}
	for _, name := range sortedKeys(users) {
		tokens[name] = e.Login(users[name], h).Token
	}
	return &caseWorld{env: e, h: h, ids: ids, tokens: tokens}
}

// seedClientPortal applies s's welcome notes, checklist items and client
// documents. Documents reach their status through the real clients store
// functions (send, withdraw, replace, sign), so seeded rows hold real
// hashes, timestamps and signing certificates.
func seedClientPortal(t *testing.T, e *testkit.Env, s casefile.Seed, orgs map[string]core.Org, ids map[string]int64, users map[string]testkit.User, store storage.Store) {
	t.Helper()
	ctx, pool := e.Ctx, e.Pool
	actor := func(org core.Org) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `SELECT user_id FROM memberships WHERE org_id = $1 AND role = 'owner' ORDER BY user_id LIMIT 1`, org.ID).Scan(&id); err != nil {
			t.Fatalf("client portal seed needs an owner in %s: %v", org.Slug, err)
		}
		return id
	}
	for _, name := range sortedKeys(s.Welcome) {
		c := s.Contacts[name]
		if _, err := clients.PutWelcome(ctx, pool, orgs[c.Org].ID, ids[name], actor(orgs[c.Org]), s.Welcome[name]); err != nil {
			t.Fatalf("welcome.%s: %v", name, err)
		}
	}
	for _, name := range sortedKeys(s.Checklist) {
		it := s.Checklist[name]
		org := orgs[it.Org]
		created, err := clients.CreateChecklistItem(ctx, pool, org.ID, ids[it.Contact], actor(org), clients.ChecklistItem{Action: it.Action})
		if err != nil {
			t.Fatalf("checklist.%s: %v", name, err)
		}
		ids[name] = created.ID
		if it.Done {
			if _, err := clients.SetChecklistDone(ctx, pool, org.ID, ids[it.Contact], actor(org), created.ID, true); err != nil {
				t.Fatalf("checklist.%s: done: %v", name, err)
			}
		}
	}
	for _, name := range sortedKeys(s.Documents) {
		doc := s.Documents[name]
		org := orgs[doc.Org]
		owner := actor(org)
		body := fmt.Sprintf("%%PDF-1.4\n%% seed document %s\n%%%%EOF\n", name)
		f, err := core.SaveFile(ctx, pool, store, org.ID, name+".pdf", strings.NewReader(body), clients.PDFPolicy)
		if err != nil {
			t.Fatalf("documents.%s: file: %v", name, err)
		}
		nd := clients.NewDocument{ContactID: ids[doc.Contact], Title: doc.Title, Kind: doc.Kind, Category: doc.Category}
		if nd.Title == "" {
			nd.Title = name
		}
		if doc.Project != "" {
			pid := ids[doc.Project]
			nd.ProjectID = &pid
		}
		created, err := clients.CreateDocument(ctx, pool, org.ID, owner, nd, f)
		if err != nil {
			t.Fatalf("documents.%s: %v", name, err)
		}
		ids[name] = created.ID
		if doc.Status == "" || doc.Status == "draft" {
			continue
		}
		// Send directly: the seed may send a signature document to a
		// contact without a portal login.
		if _, err := pool.Exec(ctx, `UPDATE client_documents SET status = 'sent', sent_at = now() WHERE id = $1`, created.ID); err != nil {
			t.Fatalf("documents.%s: send: %v", name, err)
		}
		switch doc.Status {
		case "withdrawn":
			_, err = clients.WithdrawDocument(ctx, pool, org.ID, owner, created.ID)
		case "superseded":
			if _, err = pool.Exec(ctx, `UPDATE client_documents SET status = 'superseded' WHERE id = $1`, created.ID); err != nil {
				break
			}
		case "signed":
			signer := users[doc.Signer]
			_, err = clients.Sign(ctx, pool, store, time.UTC, org.ID, ids[doc.Contact], created.ID, clients.SignInput{
				UserID: signer.ID, Email: signer.Email, TypedName: "Seed Signer", IP: "192.0.2.1", Consent: true})
		}
		if err != nil {
			t.Fatalf("documents.%s: %s: %v", name, doc.Status, err)
		}
	}
}

// runCase sends c's request as actor against w and checks the response.
// Failures name the file, case, actor and request.
func runCase(t *testing.T, file string, w *caseWorld, c casefile.Case, actor string) {
	t.Helper()
	// A copy of the Env reporting to this subtest, so a Snapshot failure
	// fails the subtest rather than the shared parent.
	e := *w.env
	e.T = t

	path, err := casefile.Interpolate(c.Request.Path, w.ids)
	if err != nil {
		t.Fatalf("%s: case %q: request.path: %v", file, c.Name, err)
	}
	body, err := casefile.InterpolateBody(c.Request.Body, w.ids)
	if err != nil {
		t.Fatalf("%s: case %q: request.body: %v", file, c.Name, err)
	}
	prefix := fmt.Sprintf("%s: case %q as %s: %s %s", file, c.Name, actor, c.Request.Method, path)

	var before string
	if len(c.Unchanged) > 0 {
		before = e.Snapshot(c.Unchanged...)
	}

	r := e.Resume(w.tokens[actor], w.h).DoWith(c.Request.Method, path, body, c.Request.Headers)

	if r.Code != c.Expect.Status {
		t.Errorf("%s: status got %d want %d\nbody: %s", prefix, r.Code, c.Expect.Status, truncate(string(r.Body)))
	}
	for _, name := range sortedKeys(c.Expect.Headers) {
		want := c.Expect.Headers[name]
		if got := r.Header.Get(name); got != want {
			t.Errorf("%s: header %s got %q want %q", prefix, name, got, want)
		}
	}
	if c.Expect.Body != nil {
		want, err := casefile.InterpolateBody(c.Expect.Body, w.ids)
		if err != nil {
			t.Fatalf("%s: expect.body: %v", prefix, err)
		}
		match := casefile.Subset
		if c.Expect.Exact {
			match = casefile.Equal
		}
		if got, err := casefile.DecodeBody(r.Body); err != nil {
			t.Errorf("%s: body is not JSON (%v)\nbody: %s", prefix, err, truncate(string(r.Body)))
		} else if ok, diff := match(want, got); !ok {
			t.Errorf("%s: body mismatch at %s\nbody: %s", prefix, diff, truncate(string(r.Body)))
		}
	}
	if len(c.Unchanged) > 0 {
		if after := e.Snapshot(c.Unchanged...); after != before {
			t.Errorf("%s: unchanged tables [%s] changed\nbefore:\n%s\nafter:\n%s",
				prefix, strings.Join(c.Unchanged, ", "), truncate(before), truncate(after))
		}
	}
}

// truncate caps s at about 2KB for failure output.
func truncate(s string) string {
	const limit = 2048
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("... (%d more bytes)", len(s)-limit)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
