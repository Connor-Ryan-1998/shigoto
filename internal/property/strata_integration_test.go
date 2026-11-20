//go:build integration

package property_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/testkit"
)

func TestStrataSchemeRoundTrips(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")

	created, err := property.CreateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{
		Name: "  Harbour View CTS ", CTSNumber: " 12345 ", IsSecretary: true,
	})
	if err != nil {
		t.Fatalf("CreateStrataScheme: %v", err)
	}
	if created.Name != "Harbour View CTS" || created.CTSNumber != "12345" {
		t.Fatalf("created = %+v", created)
	}
	if created.IsChairperson || !created.IsSecretary || created.IsTreasurer {
		t.Fatalf("positions = %+v", created)
	}
	if created.Properties == nil || len(created.Properties) != 0 {
		t.Fatalf("properties = %#v, want empty non-nil", created.Properties)
	}

	updated, err := property.UpdateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{
		ID: created.ID, Name: "Harbour View", CTSNumber: "", IsChairperson: true, IsTreasurer: true,
	})
	if err != nil {
		t.Fatalf("UpdateStrataScheme: %v", err)
	}
	got, err := property.GetStrataScheme(e.Ctx, e.Pool, org.ID, created.ID)
	if err != nil {
		t.Fatalf("GetStrataScheme: %v", err)
	}
	if !reflect.DeepEqual(got, updated) {
		t.Fatalf("get = %+v, update returned %+v", got, updated)
	}
	if got.Name != "Harbour View" || got.CTSNumber != "" || !got.IsChairperson || got.IsSecretary || !got.IsTreasurer {
		t.Fatalf("updated = %+v", got)
	}
	var stored *string
	if err := e.Pool.QueryRow(e.Ctx, `SELECT cts_number FROM strata_schemes WHERE id = $1`, got.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != nil {
		t.Fatalf("cts_number stored as %q, want NULL", *stored)
	}
}

func TestStrataSchemeRejectsInvalid(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	other := e.Org("other")
	s := e.StrataScheme(org)

	if _, err := property.CreateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{Name: "   "}); !errors.Is(err, core.ErrInvalidInput) {
		t.Errorf("create blank name: err = %v, want ErrInvalidInput", err)
	}
	if _, err := property.UpdateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{ID: s.ID, Name: "   "}); !errors.Is(err, core.ErrInvalidInput) {
		t.Errorf("update blank name: err = %v, want ErrInvalidInput", err)
	}
	if _, err := property.UpdateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{ID: s.ID + 1000, Name: "X"}); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("update unknown: err = %v, want ErrNotFound", err)
	}
	if _, err := property.UpdateStrataScheme(e.Ctx, e.Pool, other.ID, property.StrataScheme{ID: s.ID, Name: "Stolen"}); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("update cross-org: err = %v, want ErrNotFound", err)
	}
	if _, err := property.GetStrataScheme(e.Ctx, e.Pool, other.ID, s.ID); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("get cross-org: err = %v, want ErrNotFound", err)
	}
}

func TestStrataSchemeCTSUnique(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	other := e.Org("other")

	e.StrataScheme(org, testkit.SchemeName("No CTS one"))
	e.StrataScheme(org, testkit.SchemeName("No CTS two"))
	e.StrataScheme(org, testkit.SchemeName("Harbour"), testkit.CTSNumber("12345"))
	second := e.StrataScheme(org, testkit.SchemeName("Bayside"))

	_, err := property.CreateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{Name: "Dup", CTSNumber: "12345"})
	if !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "cts_number") {
		t.Errorf("create duplicate: err = %v, want ErrInvalidInput naming cts_number", err)
	}
	_, err = property.UpdateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{ID: second.ID, Name: "Bayside", CTSNumber: " 12345 "})
	if !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "cts_number") {
		t.Errorf("update duplicate: err = %v, want ErrInvalidInput naming cts_number", err)
	}
	if _, err := property.CreateStrataScheme(e.Ctx, e.Pool, other.ID, property.StrataScheme{Name: "Harbour", CTSNumber: "12345"}); err != nil {
		t.Errorf("same CTS in another org: %v", err)
	}
}

func TestSetPropertyStrataScheme(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	s := e.StrataScheme(org, testkit.SchemeName("Harbour"))
	s, err := property.UpdateStrataScheme(e.Ctx, e.Pool, org.ID, property.StrataScheme{ID: s.ID, Name: "Harbour", IsTreasurer: true})
	if err != nil {
		t.Fatal(err)
	}
	p1 := e.Property(org, testkit.PropertyName("Unit 1"))
	p2 := e.Property(org, testkit.PropertyName("Unit 2"))

	for _, p := range []property.Property{p1, p2} {
		if err := property.SetPropertyStrataScheme(e.Ctx, e.Pool, org.ID, p.ID, &s.ID); err != nil {
			t.Fatalf("link %s: %v", p.Name, err)
		}
	}
	got, err := property.GetStrataScheme(e.Ctx, e.Pool, org.ID, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []property.StrataProperty{{ID: p1.ID, Name: "Unit 1"}, {ID: p2.ID, Name: "Unit 2"}}
	if !reflect.DeepEqual(got.Properties, want) {
		t.Fatalf("properties = %+v, want %+v", got.Properties, want)
	}
	linked, _ := property.GetProperty(e.Ctx, e.Pool, org.ID, p1.ID)
	if linked.StrataSchemeID == nil || *linked.StrataSchemeID != s.ID {
		t.Fatalf("p1.StrataSchemeID = %v, want %d", linked.StrataSchemeID, s.ID)
	}

	if err := property.SetPropertyStrataScheme(e.Ctx, e.Pool, org.ID, p1.ID, nil); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	unlinked, _ := property.GetProperty(e.Ctx, e.Pool, org.ID, p1.ID)
	if unlinked.StrataSchemeID != nil {
		t.Fatalf("p1.StrataSchemeID = %v, want nil", *unlinked.StrataSchemeID)
	}
	list, err := property.ListStrataSchemes(e.Ctx, e.Pool, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].IsTreasurer || !reflect.DeepEqual(list[0].Properties, want[1:]) {
		t.Fatalf("list after unlink = %+v", list)
	}
}

func TestSetPropertyStrataSchemeCrossOrg(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	other := e.Org("other")
	p := e.Property(org)
	foreignScheme := e.StrataScheme(other)
	foreignProperty := e.Property(other)
	ownScheme := e.StrataScheme(org)

	if err := property.SetPropertyStrataScheme(e.Ctx, e.Pool, org.ID, p.ID, &foreignScheme.ID); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("foreign scheme: err = %v, want ErrNotFound", err)
	}
	if got, _ := property.GetProperty(e.Ctx, e.Pool, org.ID, p.ID); got.StrataSchemeID != nil {
		t.Errorf("property linked to %d after rejected link", *got.StrataSchemeID)
	}
	if err := property.SetPropertyStrataScheme(e.Ctx, e.Pool, org.ID, foreignProperty.ID, &ownScheme.ID); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("foreign property: err = %v, want ErrNotFound", err)
	}
}
