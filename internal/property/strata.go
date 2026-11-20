package property

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
)

// StrataScheme is a QLD strata (body corporate) scheme in which the org
// owns one or more lots. The Is* flags are the executive committee
// positions the org, as lot owner, holds in the scheme.
type StrataScheme struct {
	ID            int64
	OrgID         int64
	Name          string
	CTSNumber     string
	IsChairperson bool
	IsSecretary   bool
	IsTreasurer   bool
	// Properties are the org's properties linked to the scheme, by id.
	Properties []StrataProperty
}

// StrataProperty is a property linked to a strata scheme.
type StrataProperty struct {
	ID   int64
	Name string
}

var errDuplicateCTS = fmt.Errorf("%w: cts_number is already used by another scheme in this organisation", core.ErrInvalidInput)

const strataSchemeColumns = `id, org_id, name, coalesce(cts_number,''), is_chairperson, is_secretary, is_treasurer`

func scanStrataScheme(row pgx.Row) (StrataScheme, error) {
	var s StrataScheme
	err := row.Scan(&s.ID, &s.OrgID, &s.Name, &s.CTSNumber, &s.IsChairperson, &s.IsSecretary, &s.IsTreasurer)
	return s, err
}

// normaliseStrataScheme trims s's text fields and checks the rules
// CreateStrataScheme and UpdateStrataScheme share, including CTS number
// uniqueness within orgID.
func normaliseStrataScheme(ctx context.Context, q core.Querier, orgID int64, s StrataScheme) (StrataScheme, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.CTSNumber = strings.TrimSpace(s.CTSNumber)
	if s.Name == "" {
		return s, fmt.Errorf("%w: name is required", core.ErrInvalidInput)
	}
	if s.CTSNumber != "" {
		var taken bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM strata_schemes WHERE org_id = $1 AND cts_number = $2 AND id <> $3)`,
			orgID, s.CTSNumber, s.ID).Scan(&taken); err != nil {
			return s, err
		}
		if taken {
			return s, errDuplicateCTS
		}
	}
	return s, nil
}

// CreateStrataScheme inserts a scheme for orgID and returns it.
func CreateStrataScheme(ctx context.Context, q core.Querier, orgID int64, s StrataScheme) (StrataScheme, error) {
	s.ID = 0
	s, err := normaliseStrataScheme(ctx, q, orgID, s)
	if err != nil {
		return StrataScheme{}, err
	}
	created, err := scanStrataScheme(q.QueryRow(ctx, `
		INSERT INTO strata_schemes (org_id, name, cts_number, is_chairperson, is_secretary, is_treasurer)
		VALUES ($1, $2, NULLIF($3,''), $4, $5, $6)
		RETURNING `+strataSchemeColumns,
		orgID, s.Name, s.CTSNumber, s.IsChairperson, s.IsSecretary, s.IsTreasurer))
	if isUniqueViolation(err) {
		return StrataScheme{}, errDuplicateCTS
	}
	if err != nil {
		return StrataScheme{}, err
	}
	created.Properties = []StrataProperty{}
	return created, nil
}

// UpdateStrataScheme replaces scheme s.ID's name, CTS number and
// positions, scoped to orgID. It returns core.ErrNotFound if no such scheme
// exists in that org.
func UpdateStrataScheme(ctx context.Context, q core.Querier, orgID int64, s StrataScheme) (StrataScheme, error) {
	s, err := normaliseStrataScheme(ctx, q, orgID, s)
	if err != nil {
		return StrataScheme{}, err
	}
	_, err = scanStrataScheme(q.QueryRow(ctx, `
		UPDATE strata_schemes SET name = $3, cts_number = NULLIF($4,''),
			is_chairperson = $5, is_secretary = $6, is_treasurer = $7
		WHERE org_id = $1 AND id = $2
		RETURNING `+strataSchemeColumns,
		orgID, s.ID, s.Name, s.CTSNumber, s.IsChairperson, s.IsSecretary, s.IsTreasurer))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return StrataScheme{}, core.ErrNotFound
	case isUniqueViolation(err):
		return StrataScheme{}, errDuplicateCTS
	case err != nil:
		return StrataScheme{}, err
	}
	return GetStrataScheme(ctx, q, orgID, s.ID)
}

// GetStrataScheme looks up a scheme and its linked properties by id, scoped
// to orgID. It returns core.ErrNotFound if no such scheme exists in that
// org.
func GetStrataScheme(ctx context.Context, q core.Querier, orgID, id int64) (StrataScheme, error) {
	s, err := scanStrataScheme(q.QueryRow(ctx, `SELECT `+strataSchemeColumns+` FROM strata_schemes WHERE org_id = $1 AND id = $2`, orgID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return StrataScheme{}, core.ErrNotFound
	}
	if err != nil {
		return StrataScheme{}, err
	}
	if s.Properties, err = strataProperties(ctx, q, orgID, id); err != nil {
		return StrataScheme{}, err
	}
	return s, nil
}

// ListStrataSchemes lists orgID's schemes by name, each with its linked
// properties.
func ListStrataSchemes(ctx context.Context, q core.Querier, orgID int64) ([]StrataScheme, error) {
	rows, err := q.Query(ctx, `SELECT `+strataSchemeColumns+` FROM strata_schemes WHERE org_id = $1 ORDER BY name, id`, orgID)
	if err != nil {
		return nil, err
	}
	schemes := []StrataScheme{}
	for rows.Next() {
		s, err := scanStrataScheme(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		schemes = append(schemes, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range schemes {
		if schemes[i].Properties, err = strataProperties(ctx, q, orgID, schemes[i].ID); err != nil {
			return nil, err
		}
	}
	return schemes, nil
}

func strataProperties(ctx context.Context, q core.Querier, orgID, schemeID int64) ([]StrataProperty, error) {
	rows, err := q.Query(ctx, `SELECT id, name FROM properties WHERE org_id = $1 AND strata_scheme_id = $2 ORDER BY id`, orgID, schemeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	props := []StrataProperty{}
	for rows.Next() {
		var p StrataProperty
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		props = append(props, p)
	}
	return props, rows.Err()
}

// SetPropertyStrataScheme links property propertyID to scheme schemeID, or
// unlinks it when schemeID is nil; unlinking keeps the scheme. Both must be
// in orgID, otherwise it returns core.ErrNotFound.
func SetPropertyStrataScheme(ctx context.Context, q core.Querier, orgID, propertyID int64, schemeID *int64) error {
	if schemeID != nil {
		if _, err := GetStrataScheme(ctx, q, orgID, *schemeID); err != nil {
			return err
		}
	}
	tag, err := q.Exec(ctx, `UPDATE properties SET strata_scheme_id = $3 WHERE org_id = $1 AND id = $2`, orgID, propertyID, schemeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return nil
}
