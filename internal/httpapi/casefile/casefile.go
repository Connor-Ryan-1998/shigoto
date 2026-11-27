// Package casefile loads and validates the YAML HTTP case files under
// internal/httpapi/testdata/cases. A file declares a seed (orgs, users and
// records, each named) and a list of cases: one request sent as one or more
// actors, with the expected status, body subset and headers, plus the
// tables the request must leave unchanged.
//
// The format is deliberately flat: no loops, conditionals or step
// sequencing. The only expansion is a list under `as:`; the only
// interpolation is `{seedName}`, which resolves to the seeded row's ID.
// Unknown keys, unknown names and shared-seed writes are load errors, so a
// typo cannot silently turn into a passing "not found" case.
package casefile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"shigoto/internal/config"
)

// Anonymous is the actor name for a request sent without a session.
const Anonymous = "anonymous"

// File is one YAML case file.
type File struct {
	Path  string `yaml:"-"`
	Seed  Seed   `yaml:"seed"`
	Cases []Case `yaml:"cases"`
}

// Seed is the data created before a file's cases run. Every map key is a
// seed name; names are unique across Users, Contacts, Invoices,
// StrataSchemes, Properties, Leases, Projects, Tasks and Invites and resolve
// to row IDs in `{name}` tokens.
type Seed struct {
	Orgs       []string                `yaml:"orgs"`
	Modules    map[string]bool         `yaml:"modules"` // nil: all enabled
	Users      map[string]SeedUser     `yaml:"users"`   // key is the actor name
	Contacts   map[string]SeedContact  `yaml:"contacts"`
	Invoices   map[string]SeedInvoice  `yaml:"invoices"`
	Properties map[string]SeedProperty `yaml:"properties"`
	// StrataSchemes are seeded before Properties, which may link to them.
	StrataSchemes map[string]SeedStrataScheme `yaml:"strata_schemes"`
	// StrataDocuments are seeded after StrataSchemes.
	StrataDocuments map[string]SeedStrataDocument `yaml:"strata_documents"`
	Leases          map[string]SeedLease          `yaml:"leases"`
	Payments        map[string]SeedPayment        `yaml:"payments"`
	Tickets         map[string]SeedTicket         `yaml:"tickets"`
	Projects        map[string]SeedProject        `yaml:"projects"`
	Tasks           map[string]SeedTask           `yaml:"tasks"`
	Invites         map[string]SeedInvite         `yaml:"invites"`
	Settings        map[string]SeedSettings       `yaml:"settings"` // key is the org slug
	// Welcome is a client contact's welcome note, keyed by seed contact.
	Welcome   map[string]string            `yaml:"welcome"`
	Checklist map[string]SeedChecklistItem `yaml:"checklist"`
	Documents map[string]SeedDocument      `yaml:"documents"`
	// Quotes are seeded after projects and client documents.
	Quotes map[string]SeedQuote `yaml:"quotes"`
	// Assistant, when set, configures the strata assistant against a
	// scripted model that answers every request with Reply.
	Assistant *SeedAssistant `yaml:"assistant"`
}

// SeedAssistant enables the strata assistant with a fixed model reply.
type SeedAssistant struct {
	Reply string `yaml:"reply"`
}

// SeedChecklistItem is a kickoff checklist item for the client Contact.
type SeedChecklistItem struct {
	Org     string `yaml:"org"`
	Contact string `yaml:"contact"`
	Action  string `yaml:"action"`
	Done    bool   `yaml:"done"`
}

// SeedDocument is a client document (a tiny generated PDF) for the client
// Contact, optionally on the seed project Project. Status defaults to
// draft; signed needs Signer, the seed client user who signs it. Kind is
// signature or shared; Category defaults to general.
type SeedDocument struct {
	Org      string `yaml:"org"`
	Contact  string `yaml:"contact"`
	Project  string `yaml:"project"`
	Title    string `yaml:"title"`
	Kind     string `yaml:"kind"`
	Category string `yaml:"category"`
	Status   string `yaml:"status"`
	Signer   string `yaml:"signer"`
}

// SeedQuote is a quote for client Contact, optionally on Project (which
// must be Contact's). It has one GST-free labour line, 1 hour at 10000
// cents. Status (default draft) is reached by sending it (storing its PDF)
// and then setting the status; ValidUntil (YYYY-MM-DD) defaults to 30 days
// from today, and the quote is issued 30 days before it.
type SeedQuote struct {
	Org        string `yaml:"org"`
	Contact    string `yaml:"contact"`
	Project    string `yaml:"project"`
	Title      string `yaml:"title"`
	Status     string `yaml:"status"`
	ValidUntil string `yaml:"valid_until"`
}

// SeedUser is a member of Org with Role. Contact (a seed contact name in
// the same org) is required for tenant and client, and rejected otherwise.
type SeedUser struct {
	Org     string `yaml:"org"`
	Role    string `yaml:"role"`
	Contact string `yaml:"contact"`
}

// SeedContact is a contact in Org. Empty fields take testkit defaults.
type SeedContact struct {
	Org   string `yaml:"org"`
	Kind  string `yaml:"kind"`
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// SeedInvoice is an invoice in Org to the seed contact Contact. Status is
// draft (the default) or issued; Amount 0 takes the testkit default.
type SeedInvoice struct {
	Org     string `yaml:"org"`
	Contact string `yaml:"contact"`
	Status  string `yaml:"status"`
	Source  string `yaml:"source"`
	Amount  int64  `yaml:"amount"`
	// Project, when set, puts the invoice's lines on that seed project.
	Project string `yaml:"project"`
}

// SeedProperty is a property in Org, residential unless Commercial.
// StrataScheme, when set, names a seed strata scheme in the same org the
// property is linked to.
type SeedProperty struct {
	Org          string `yaml:"org"`
	Name         string `yaml:"name"`
	Commercial   bool   `yaml:"commercial"`
	StrataScheme string `yaml:"strata_scheme"`
}

// SeedStrataScheme is a strata scheme in Org holding Positions (any of
// chairperson, secretary, treasurer). CTS is its optional community titles
// scheme number.
type SeedStrataScheme struct {
	Org       string   `yaml:"org"`
	Name      string   `yaml:"name"`
	CTS       string   `yaml:"cts"`
	Positions []string `yaml:"positions"`
}

// StrataPositions are the positions a seed strata scheme may hold.
var StrataPositions = []string{"chairperson", "secretary", "treasurer"}

// StrataDocTypes and StrataDocStatuses are the values a seed strata
// document may take (empty means minutes / pending).
var (
	StrataDocTypes    = []string{"minutes", "by_laws", "cms", "levy_notice", "budget", "financial_statement", "sinking_fund_forecast", "other"}
	StrataDocStatuses = []string{"pending", "extracting", "indexed", "failed"}
)

// SeedStrataDocument is a document of the seed strata scheme Scheme (in
// the scheme's org): a generated PDF of Pages pages (default 1), with
// Type (default minutes), Title (default "AGM 2024"), Date (default
// 2024-09-12) and Status (default pending).
type SeedStrataDocument struct {
	Scheme string `yaml:"scheme"`
	Type   string `yaml:"type"`
	Title  string `yaml:"title"`
	Date   string `yaml:"date"`
	Pages  int    `yaml:"pages"`
	Status string `yaml:"status"`
}

// SeedLease is a lease on the seed property Property with the seed contact
// Tenant as primary tenant and CoTenants as further tenants. Rent 0 takes
// the testkit default. RentRun runs rent as of the lease start and issues
// every period invoice it creates.
type SeedLease struct {
	Org       string   `yaml:"org"`
	Property  string   `yaml:"property"`
	Tenant    string   `yaml:"tenant"`
	CoTenants []string `yaml:"co_tenants"`
	Rent      int64    `yaml:"rent"`
	RentRun   bool     `yaml:"rent_run"`
	// Terminated (YYYY-MM-DD) sets the lease's termination date.
	Terminated string `yaml:"terminated"`
}

// SeedTicket is a maintenance ticket on the seed lease Lease raised by the
// seed contact RaisedBy (a tenant on it). Status (default new) is set
// directly; Note adds a staff-only note.
type SeedTicket struct {
	Org      string `yaml:"org"`
	Lease    string `yaml:"lease"`
	RaisedBy string `yaml:"raised_by"`
	Title    string `yaml:"title"`
	Status   string `yaml:"status"`
	Note     string `yaml:"note"`
	// Supplier (a seed supplier contact) is assigned; Visit then books a
	// visit with them on 2030-01-07 09:00-11:00 UTC, its ID named
	// "<ticket>_visit".
	Supplier string `yaml:"supplier"`
	Visit    bool   `yaml:"visit"`
}

// SeedPayment is a bank-transfer payment of Amount against the Period-th
// (0-based) rent invoice of the seed lease Lease, which needs rent_run.
type SeedPayment struct {
	Org    string `yaml:"org"`
	Lease  string `yaml:"lease"`
	Period int    `yaml:"period"`
	Amount int64  `yaml:"amount"`
}

// SeedProject is a timesheets project for the seed contact Client.
// Name and Status are optional (testkit defaults).
type SeedProject struct {
	Org    string `yaml:"org"`
	Client string `yaml:"client"`
	Name   string `yaml:"name"`
	Status string `yaml:"status"`
}

// SeedTask is a task in the seed project Project. Empty Name takes the
// testkit default; Rate 0 means no override (the project's rate applies).
type SeedTask struct {
	Org     string `yaml:"org"`
	Project string `yaml:"project"`
	Name    string `yaml:"name"`
	Rate    int64  `yaml:"rate"`
}

// SeedInvite is an outstanding staff invite to Org for Email with Role.
// Token is the plaintext invite token, written literally into accept paths
// (`/api/v1/invites/<token>`); it must be unique within the file. Expired
// makes the invite already past its expiry.
type SeedInvite struct {
	Org     string `yaml:"org"`
	Email   string `yaml:"email"`
	Role    string `yaml:"role"`
	Token   string `yaml:"token"`
	Expired bool   `yaml:"expired"`
}

// SeedSettings sets org flags directly.
type SeedSettings struct {
	GSTRegistered  bool   `yaml:"gst_registered"`
	PayOptions     bool   `yaml:"pay_options"`
	EmergencyPhone string `yaml:"emergency_phone"`
}

// Case is one request, run once per actor in As.
type Case struct {
	Name      string   `yaml:"name"`
	As        Actors   `yaml:"as"` // scalar or list; "anonymous" allowed
	Request   Request  `yaml:"request"`
	Expect    Expect   `yaml:"expect"`
	Unchanged []string `yaml:"unchanged"`
	Isolate   bool     `yaml:"isolate"`
}

// Actors is the `as:` list. It accepts a scalar or a sequence.
type Actors []string

// UnmarshalYAML accepts `as: owner` or `as: [owner, keeper]`.
func (a *Actors) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*a = Actors{n.Value}
		return nil
	case yaml.SequenceNode:
		out := make(Actors, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: as: each actor must be a name", c.Line)
			}
			out = append(out, c.Value)
		}
		*a = out
		return nil
	default:
		return fmt.Errorf("line %d: as: want an actor name or a list of names", n.Line)
	}
}

// Request is the HTTP request a case sends. Path and Body may contain
// `{seedName}` tokens. Headers are applied after the runner's defaults.
type Request struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Body    any               `yaml:"body"`
	Headers map[string]string `yaml:"headers"`
}

// Expect is what the response must look like. Body is a subset match (see
// Subset), or with Exact a whole-body match (see Equal), and may contain
// `{seedName}` tokens; Headers are exact values for the listed names only.
type Expect struct {
	Status  int               `yaml:"status"`
	Body    any               `yaml:"body"`
	Exact   bool              `yaml:"exact"`
	Headers map[string]string `yaml:"headers"`
}

var methods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

var roles = []string{"owner", "staff", "bookkeeper", "tenant", "client"}

var inviteRoles = []string{"owner", "staff", "bookkeeper"}

// Load reads and strictly decodes path (unknown keys fail), then validates it.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	f.Path = path
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

// seedNames returns every interpolatable seed name, each mapped to the name
// of the seed map it came from.
func (s Seed) seedNames() map[string]string {
	names := map[string]string{}
	add := func(kind string, keys []string) {
		for _, k := range keys {
			if _, dup := names[k]; !dup {
				names[k] = kind
			}
		}
	}
	add("users", sortedKeys(s.Users))
	add("contacts", sortedKeys(s.Contacts))
	add("invoices", sortedKeys(s.Invoices))
	add("strata_schemes", sortedKeys(s.StrataSchemes))
	add("strata_documents", sortedKeys(s.StrataDocuments))
	add("properties", sortedKeys(s.Properties))
	add("leases", sortedKeys(s.Leases))
	add("payments", sortedKeys(s.Payments))
	add("tickets", sortedKeys(s.Tickets))
	var visits []string
	for _, name := range sortedKeys(s.Tickets) {
		if s.Tickets[name].Visit {
			visits = append(visits, name+"_visit")
		}
	}
	add("visits", visits)
	add("projects", sortedKeys(s.Projects))
	add("tasks", sortedKeys(s.Tasks))
	add("invites", sortedKeys(s.Invites))
	add("checklist", sortedKeys(s.Checklist))
	add("documents", sortedKeys(s.Documents))
	add("quotes", sortedKeys(s.Quotes))
	return names
}

// Validate reports every problem in f, each prefixed with the file name and
// the seed entry or case it concerns.
func (f File) Validate() error {
	file := filepath.Base(f.Path)
	var errs []error
	seedErr := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: seed: %s", file, fmt.Sprintf(format, args...)))
	}
	s := f.Seed

	orgs := map[string]bool{}
	for _, o := range s.Orgs {
		if o == "" {
			seedErr("orgs: empty slug")
		}
		if orgs[o] {
			seedErr("orgs: duplicate org %q", o)
		}
		orgs[o] = true
	}
	checkOrg := func(where, org string) {
		if !orgs[org] {
			seedErr("%s: unknown org %q", where, org)
		}
	}
	for _, m := range sortedKeys(s.Modules) {
		if !slices.Contains(config.Known, m) {
			seedErr("modules: unknown module %q (known: %s)", m, strings.Join(config.Known, ", "))
		}
	}

	// Names must be unique across the interpolatable maps.
	seen := map[string]string{}
	for _, group := range []struct {
		kind string
		keys []string
	}{
		{"users", sortedKeys(s.Users)},
		{"contacts", sortedKeys(s.Contacts)},
		{"invoices", sortedKeys(s.Invoices)},
		{"strata_schemes", sortedKeys(s.StrataSchemes)},
		{"strata_documents", sortedKeys(s.StrataDocuments)},
		{"properties", sortedKeys(s.Properties)},
		{"leases", sortedKeys(s.Leases)},
		{"payments", sortedKeys(s.Payments)},
		{"tickets", sortedKeys(s.Tickets)},
		{"projects", sortedKeys(s.Projects)},
		{"tasks", sortedKeys(s.Tasks)},
		{"invites", sortedKeys(s.Invites)},
		{"checklist", sortedKeys(s.Checklist)},
		{"documents", sortedKeys(s.Documents)},
		{"quotes", sortedKeys(s.Quotes)},
	} {
		for _, k := range group.keys {
			if prev, dup := seen[k]; dup {
				seedErr("duplicate name %q in %s and %s", k, prev, group.kind)
				continue
			}
			seen[k] = group.kind
		}
	}
	if _, ok := s.Users[Anonymous]; ok {
		seedErr("users: %q is reserved for requests without a session", Anonymous)
	}

	// checkContact requires ref to be a seed contact in org.
	checkContact := func(where, field, ref, org string) {
		c, ok := s.Contacts[ref]
		switch {
		case ref == "":
			seedErr("%s: %s is required", where, field)
		case !ok:
			seedErr("%s: %s: unknown contact %q", where, field, ref)
		case c.Org != org:
			seedErr("%s: %s: contact %q is in org %q, not %q", where, field, ref, c.Org, org)
		}
	}

	for _, name := range sortedKeys(s.Users) {
		u := s.Users[name]
		where := "users." + name
		checkOrg(where, u.Org)
		if !slices.Contains(roles, u.Role) {
			seedErr("%s: unknown role %q (known: %s)", where, u.Role, strings.Join(roles, ", "))
			continue
		}
		if u.Role == "tenant" || u.Role == "client" {
			checkContact(where, "contact", u.Contact, u.Org)
		} else if u.Contact != "" {
			seedErr("%s: contact is only for tenant and client users, not %s", where, u.Role)
		}
	}
	for _, name := range sortedKeys(s.Contacts) {
		checkOrg("contacts."+name, s.Contacts[name].Org)
	}
	for _, name := range sortedKeys(s.Invoices) {
		inv := s.Invoices[name]
		where := "invoices." + name
		checkOrg(where, inv.Org)
		checkContact(where, "contact", inv.Contact, inv.Org)
		switch inv.Status {
		case "", "draft", "issued":
		default:
			seedErr("%s: status %q must be draft or issued", where, inv.Status)
		}
		if inv.Amount < 0 {
			seedErr("%s: negative amount %d", where, inv.Amount)
		}
		if inv.Project != "" {
			if p, ok := s.Projects[inv.Project]; !ok {
				seedErr("%s: project: unknown project %q", where, inv.Project)
			} else if p.Org != inv.Org {
				seedErr("%s: project: project %q is in org %q, not %q", where, inv.Project, p.Org, inv.Org)
			}
		}
	}
	for _, name := range sortedKeys(s.Welcome) {
		c, ok := s.Contacts[name]
		if !ok {
			seedErr("welcome.%s: unknown contact %q", name, name)
		} else if c.Kind != "client" {
			seedErr("welcome.%s: contact %q is not a client", name, name)
		}
	}
	for _, name := range sortedKeys(s.Checklist) {
		it := s.Checklist[name]
		where := "checklist." + name
		checkOrg(where, it.Org)
		checkContact(where, "contact", it.Contact, it.Org)
		if strings.TrimSpace(it.Action) == "" {
			seedErr("%s: action is required", where)
		}
	}
	for _, name := range sortedKeys(s.Quotes) {
		q := s.Quotes[name]
		where := "quotes." + name
		checkOrg(where, q.Org)
		checkContact(where, "contact", q.Contact, q.Org)
		if q.Project != "" {
			if p, ok := s.Projects[q.Project]; !ok {
				seedErr("%s: project: unknown project %q", where, q.Project)
			} else if p.Client != q.Contact {
				seedErr("%s: project: project %q is not contact %q's", where, q.Project, q.Contact)
			}
		}
		switch q.Status {
		case "", "draft", "sent", "accepted", "declined", "superseded":
		default:
			seedErr("%s: unknown status %q", where, q.Status)
		}
		if q.ValidUntil != "" {
			if _, err := time.Parse(time.DateOnly, q.ValidUntil); err != nil {
				seedErr("%s: valid_until %q is not YYYY-MM-DD", where, q.ValidUntil)
			}
		}
	}
	for _, name := range sortedKeys(s.Documents) {
		doc := s.Documents[name]
		where := "documents." + name
		checkOrg(where, doc.Org)
		checkContact(where, "contact", doc.Contact, doc.Org)
		if doc.Project != "" {
			if p, ok := s.Projects[doc.Project]; !ok {
				seedErr("%s: project: unknown project %q", where, doc.Project)
			} else if p.Client != doc.Contact {
				seedErr("%s: project: project %q is not contact %q's", where, doc.Project, doc.Contact)
			}
		}
		if doc.Kind != "signature" && doc.Kind != "shared" {
			seedErr("%s: kind %q must be signature or shared", where, doc.Kind)
		}
		switch doc.Status {
		case "", "draft", "sent", "withdrawn", "superseded":
			if doc.Signer != "" {
				seedErr("%s: signer is only for signed documents", where)
			}
		case "signed":
			if doc.Kind != "signature" {
				seedErr("%s: only signature documents can be signed", where)
			}
			if u, ok := s.Users[doc.Signer]; !ok || u.Role != "client" || u.Contact != doc.Contact {
				seedErr("%s: signer %q must be a client user for contact %q", where, doc.Signer, doc.Contact)
			}
		default:
			seedErr("%s: unknown status %q", where, doc.Status)
		}
	}
	for _, name := range sortedKeys(s.StrataSchemes) {
		sc := s.StrataSchemes[name]
		checkOrg("strata_schemes."+name, sc.Org)
		for _, p := range sc.Positions {
			if !slices.Contains(StrataPositions, p) {
				seedErr("strata_schemes.%s: unknown position %q (known: %s)", name, p, strings.Join(StrataPositions, ", "))
			}
		}
	}
	if s.Assistant != nil && s.Assistant.Reply == "" {
		seedErr("assistant: reply is required")
	}
	for _, name := range sortedKeys(s.StrataDocuments) {
		d := s.StrataDocuments[name]
		where := "strata_documents." + name
		if _, ok := s.StrataSchemes[d.Scheme]; !ok {
			seedErr("%s: scheme: unknown strata scheme %q", where, d.Scheme)
		}
		if d.Type != "" && !slices.Contains(StrataDocTypes, d.Type) {
			seedErr("%s: unknown type %q", where, d.Type)
		}
		if d.Status != "" && !slices.Contains(StrataDocStatuses, d.Status) {
			seedErr("%s: unknown status %q", where, d.Status)
		}
		if d.Pages < 0 {
			seedErr("%s: negative pages %d", where, d.Pages)
		}
	}
	for _, name := range sortedKeys(s.Properties) {
		p := s.Properties[name]
		where := "properties." + name
		checkOrg(where, p.Org)
		if p.StrataScheme == "" {
			continue
		}
		if sc, ok := s.StrataSchemes[p.StrataScheme]; !ok {
			seedErr("%s: strata_scheme: unknown strata scheme %q", where, p.StrataScheme)
		} else if sc.Org != p.Org {
			seedErr("%s: strata_scheme: scheme %q is in org %q, not %q", where, p.StrataScheme, sc.Org, p.Org)
		}
	}
	for _, name := range sortedKeys(s.Leases) {
		l := s.Leases[name]
		where := "leases." + name
		checkOrg(where, l.Org)
		if p, ok := s.Properties[l.Property]; !ok {
			seedErr("%s: property: unknown property %q", where, l.Property)
		} else if p.Org != l.Org {
			seedErr("%s: property: property %q is in org %q, not %q", where, l.Property, p.Org, l.Org)
		}
		checkContact(where, "tenant", l.Tenant, l.Org)
		for _, co := range l.CoTenants {
			checkContact(where, "co_tenants", co, l.Org)
		}
		if l.Rent < 0 {
			seedErr("%s: negative rent %d", where, l.Rent)
		}
	}
	for _, name := range sortedKeys(s.Payments) {
		p := s.Payments[name]
		where := "payments." + name
		checkOrg(where, p.Org)
		l, ok := s.Leases[p.Lease]
		switch {
		case !ok:
			seedErr("%s: lease: unknown lease %q", where, p.Lease)
		case l.Org != p.Org:
			seedErr("%s: lease: lease %q is in org %q, not %q", where, p.Lease, l.Org, p.Org)
		case !l.RentRun:
			seedErr("%s: lease %q needs rent_run: true", where, p.Lease)
		}
		if p.Amount <= 0 || p.Period < 0 {
			seedErr("%s: amount must be positive and period non-negative", where)
		}
	}
	for _, name := range sortedKeys(s.Tickets) {
		tk := s.Tickets[name]
		where := "tickets." + name
		checkOrg(where, tk.Org)
		if l, ok := s.Leases[tk.Lease]; !ok || l.Org != tk.Org {
			seedErr("%s: lease: unknown lease %q in org %q", where, tk.Lease, tk.Org)
		}
		checkContact(where, "raised_by", tk.RaisedBy, tk.Org)
		if tk.Supplier != "" {
			checkContact(where, "supplier", tk.Supplier, tk.Org)
		}
		if tk.Visit && tk.Supplier == "" {
			seedErr("%s: visit needs a supplier", where)
		}
		switch tk.Status {
		case "", "new", "viewed", "assigned", "scheduled", "completed", "cancelled":
		default:
			seedErr("%s: unknown status %q", where, tk.Status)
		}
	}
	for _, name := range sortedKeys(s.Projects) {
		p := s.Projects[name]
		where := "projects." + name
		checkOrg(where, p.Org)
		checkContact(where, "client", p.Client, p.Org)
		switch p.Status {
		case "", "pending", "in_progress", "completed", "cancelled":
		default:
			seedErr("%s: unknown status %q", where, p.Status)
		}
	}
	for _, name := range sortedKeys(s.Tasks) {
		tk := s.Tasks[name]
		where := "tasks." + name
		checkOrg(where, tk.Org)
		if p, ok := s.Projects[tk.Project]; !ok {
			seedErr("%s: project: unknown project %q", where, tk.Project)
		} else if p.Org != tk.Org {
			seedErr("%s: project: project %q is in org %q, not %q", where, tk.Project, p.Org, tk.Org)
		}
		if tk.Rate < 0 {
			seedErr("%s: negative rate %d", where, tk.Rate)
		}
	}
	tokens := map[string]string{}
	for _, name := range sortedKeys(s.Invites) {
		inv := s.Invites[name]
		where := "invites." + name
		checkOrg(where, inv.Org)
		if inv.Email == "" {
			seedErr("%s: email is required", where)
		}
		if !slices.Contains(inviteRoles, inv.Role) {
			seedErr("%s: role %q must be one of %s", where, inv.Role, strings.Join(inviteRoles, ", "))
		}
		switch prev, dup := tokens[inv.Token]; {
		case inv.Token == "":
			seedErr("%s: token is required", where)
		case dup:
			seedErr("%s: token %q is already used by invites.%s", where, inv.Token, prev)
		default:
			tokens[inv.Token] = name
		}
	}
	for _, org := range sortedKeys(s.Settings) {
		checkOrg("settings."+org, org)
	}

	// Cases. Interpolating against zero IDs checks every token is a name.
	ids := map[string]int64{}
	for name := range s.seedNames() {
		ids[name] = 0
	}
	names := map[string]bool{}
	for i, c := range f.Cases {
		label := fmt.Sprintf("case %q", c.Name)
		if c.Name == "" {
			label = fmt.Sprintf("case #%d", i+1)
		}
		caseErr := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("%s: %s: %s", file, label, fmt.Sprintf(format, args...)))
		}
		if c.Name == "" {
			caseErr("name is required")
		} else if names[c.Name] {
			caseErr("duplicate case name")
		}
		names[c.Name] = true

		if len(c.As) == 0 {
			caseErr("as: at least one actor is required")
		}
		actors := map[string]bool{}
		for _, a := range c.As {
			if _, ok := s.Users[a]; !ok && a != Anonymous {
				caseErr("as: unknown actor %q (not in seed.users and not %q)", a, Anonymous)
			}
			if actors[a] {
				caseErr("as: duplicate actor %q", a)
			}
			actors[a] = true
		}

		if !slices.Contains(methods, c.Request.Method) {
			caseErr("request.method %q must be one of %s", c.Request.Method, strings.Join(methods, ", "))
		}
		if !strings.HasPrefix(c.Request.Path, "/") {
			caseErr("request.path %q must start with /", c.Request.Path)
		}
		if _, err := Interpolate(c.Request.Path, ids); err != nil {
			caseErr("request.path: %v", err)
		}
		if _, err := InterpolateBody(c.Request.Body, ids); err != nil {
			caseErr("request.body: %v", err)
		}
		if _, err := InterpolateBody(c.Expect.Body, ids); err != nil {
			caseErr("expect.body: %v", err)
		}

		if c.Expect.Exact && c.Expect.Body == nil {
			caseErr("expect.exact needs an expect.body")
		}
		if c.Expect.Status == 0 {
			caseErr("expect.status is required")
		} else if c.Expect.Status < 100 || c.Expect.Status > 599 {
			caseErr("expect.status %d is not an HTTP status", c.Expect.Status)
		}
		write := c.Request.Method != "GET" && c.Request.Method != "HEAD"
		if write && c.Expect.Status >= 200 && c.Expect.Status < 300 && !c.Isolate {
			caseErr("%s expecting %d writes to the shared seed; set isolate: true", c.Request.Method, c.Expect.Status)
		}
		for _, tbl := range c.Unchanged {
			if tbl == "" {
				caseErr("unchanged: empty table name")
			}
		}
	}
	return errors.Join(errs...)
}

// Interpolate replaces each `{name}` in s with ids[name]. A `{` that does
// not open a known name (or is never closed) is an error naming the token.
func Interpolate(s string, ids map[string]int64) (string, error) {
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		b.WriteString(s[:open])
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			return "", fmt.Errorf("unclosed token %q", s[open:])
		}
		token := s[open+1 : open+end]
		id, ok := ids[token]
		if !ok {
			return "", fmt.Errorf("unknown seed name {%s}", token)
		}
		b.WriteString(strconv.FormatInt(id, 10))
		s = s[open+end+1:]
	}
}

// InterpolateBody returns a copy of v with every string interpolated. A
// string that is exactly one `{name}` token becomes the int64 ID, so
// `contact_id: "{c_acme}"` sends a JSON number. Map keys are not
// interpolated.
func InterpolateBody(v any, ids map[string]int64) (any, error) {
	switch t := v.(type) {
	case string:
		if len(t) > 2 && t[0] == '{' && t[len(t)-1] == '}' && !strings.ContainsAny(t[1:len(t)-1], "{}") {
			id, ok := ids[t[1:len(t)-1]]
			if !ok {
				return nil, fmt.Errorf("unknown seed name %s", t)
			}
			return id, nil
		}
		return Interpolate(t, ids)
	case map[string]any:
		out := make(map[string]any, len(t))
		for _, k := range sortedKeys(t) {
			iv, err := InterpolateBody(t[k], ids)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = iv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			iv, err := InterpolateBody(e, ids)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = iv
		}
		return out, nil
	default:
		return v, nil
	}
}

// Equal reports whether got equals want exactly, after the same JSON
// normalisation as Subset: extra keys in got objects fail too. diff names
// the JSON path of the first difference.
func Equal(want, got any) (ok bool, diff string) {
	return compare(want, got, true)
}

// Subset reports whether want is contained in got, after normalising both
// through a JSON round trip: every wanted map key must be present with a
// matching value (extra keys in got are ignored), slices must have equal
// length and match element-wise, and scalars must be equal, with numbers
// compared exactly by value and never equal to strings. diff names the
// JSON path of the first mismatch.
func Subset(want, got any) (ok bool, diff string) {
	return compare(want, got, false)
}

func compare(want, got any, exact bool) (ok bool, diff string) {
	w, err := normalise(want)
	if err != nil {
		return false, fmt.Sprintf("encoding expected body: %v", err)
	}
	g, err := normalise(got)
	if err != nil {
		return false, fmt.Sprintf("encoding actual body: %v", err)
	}
	if d := subset(w, g, "", exact); d != "" {
		return false, d
	}
	return true, ""
}

func normalise(v any) (any, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// subset returns the first place got does not contain want, or "". With
// exact, a key in a got object that want lacks is also a difference.
func subset(want, got any, path string, exact bool) string {
	at := path
	if at == "" {
		at = "(root)"
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: got %s, want an object", at, show(got))
		}
		for _, k := range sortedKeys(w) {
			child := k
			if path != "" {
				child = path + "." + k
			}
			gv, present := g[k]
			if !present {
				return fmt.Sprintf("%s: missing key (want %s)", child, show(w[k]))
			}
			if d := subset(w[k], gv, child, exact); d != "" {
				return d
			}
		}
		if exact {
			for _, k := range sortedKeys(g) {
				if _, wanted := w[k]; !wanted {
					child := k
					if path != "" {
						child = path + "." + k
					}
					return fmt.Sprintf("%s: unexpected key (got %s)", child, show(g[k]))
				}
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: got %s, want an array", at, show(got))
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s: array length got %d want %d (got %s)", at, len(g), len(w), show(got))
		}
		for i := range w {
			if d := subset(w[i], g[i], fmt.Sprintf("%s[%d]", path, i), exact); d != "" {
				return d
			}
		}
		return ""
	case json.Number:
		g, ok := got.(json.Number)
		if !ok || !numbersEqual(w, g) {
			return fmt.Sprintf("%s: got %s, want %s", at, show(got), show(want))
		}
		return ""
	default:
		if want != got {
			return fmt.Sprintf("%s: got %s, want %s", at, show(got), show(want))
		}
		return ""
	}
}

func numbersEqual(a, b json.Number) bool {
	x, okx := new(big.Rat).SetString(a.String())
	y, oky := new(big.Rat).SetString(b.String())
	return okx && oky && x.Cmp(y) == 0
}

// show renders a normalised value as compact JSON, with its type for scalars
// so `"1000" (string)` and `1000 (number)` are distinguishable.
func show(v any) string {
	buf, _ := json.Marshal(v)
	s := string(buf)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	switch v.(type) {
	case string:
		return s + " (string)"
	case json.Number:
		return s + " (number)"
	case bool:
		return s + " (bool)"
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DecodeBody decodes a JSON response body for Subset/Equal, keeping numbers
// as json.Number so integers above 2^53 compare exactly. Trailing data after
// the first JSON value is an error.
func DecodeBody(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var got any
	if err := dec.Decode(&got); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after JSON value")
	}
	return got, nil
}
