package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// OrgSettings is the full set of an org's editable settings: identity,
// GST/BAS configuration, bank/PayID payment details and rent-invoicing
// defaults.
type OrgSettings struct {
	Name            string
	ABN             string
	GSTRegistered   bool
	BASFrequency    string
	BankBSB         string
	BankAccount     string
	PayID           string
	InvoiceLeadDays int
	AutoSendRent    bool
	// EmergencyPhone is shown to tenants who mark a repair as urgent.
	EmergencyPhone string

	// Business profile, printed on quotes.
	Address    string
	Phone      string
	Email      string
	QuoteTerms string
	Licences   []Licence
	// LogoFileID is set only through SetOrgLogo; UpdateSettings ignores it.
	LogoFileID *int64
}

// Licence is one trade or business licence, e.g. {QBCC, 1234567}. Type is
// free-form so any industry fits.
type Licence struct {
	Type   string `json:"type"`
	Number string `json:"number"`
}

// Profile limits.
const (
	maxLicences     = 5
	maxLicenceField = 60
	maxAddress      = 300
	maxQuoteTerms   = 4000
)

// LogoPolicy is for the org logo: PNG or JPEG up to 2 MiB.
var LogoPolicy = UploadPolicy{
	MaxBytes: 2 << 20,
	Allowed:  map[string]bool{"image/png": true, "image/jpeg": true},
}

var (
	bsbPattern      = regexp.MustCompile(`^\d{3}-?\d{3}$`)
	accountPattern  = regexp.MustCompile(`^\d{6,10}$`)
	auMobilePattern = regexp.MustCompile(`^(\+61|0)4\d{8}$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9 ()-]{8,20}$`)
)

// ValidABN reports whether s is a valid Australian Business Number: 11
// digits once spaces are stripped, passing the ATO's modulus-89 checksum
// (subtract 1 from the first digit, multiply by the weights
// 10,1,3,5,7,9,11,13,15,17,19, and the sum must be divisible by 89). An
// empty ABN is allowed - it means "not provided".
func ValidABN(s string) bool {
	stripped := strings.ReplaceAll(s, " ", "")
	if stripped == "" {
		return true
	}
	if len(stripped) != 11 {
		return false
	}
	digits := make([]int, 11)
	for i, c := range stripped {
		if c < '0' || c > '9' {
			return false
		}
		digits[i] = int(c - '0')
	}
	digits[0]--
	weights := [11]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for i, w := range weights {
		sum += digits[i] * w
	}
	return sum%89 == 0
}

// validPayID reports whether s is a usable PayID: an email address, an AU
// mobile number (spaces allowed), or a valid ABN. Callers must check
// length and emptiness themselves.
func validPayID(s string) bool {
	if _, err := mail.ParseAddress(s); err == nil {
		return true
	}
	if auMobilePattern.MatchString(strings.ReplaceAll(s, " ", "")) {
		return true
	}
	return s != "" && ValidABN(s)
}

// validateSettings checks in against every org-settings rule and returns a
// normalized copy (bank_bsb as "ddd-ddd") ready to store. On failure it
// returns ErrInvalidInput wrapped with the offending field's name.
func validateSettings(in OrgSettings) (OrgSettings, error) {
	if in.Name == "" {
		return OrgSettings{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if !ValidABN(in.ABN) {
		return OrgSettings{}, fmt.Errorf("%w: invalid abn", ErrInvalidInput)
	}

	switch in.BASFrequency {
	case "", "quarterly", "monthly":
	default:
		return OrgSettings{}, fmt.Errorf("%w: invalid bas_frequency", ErrInvalidInput)
	}

	bsb := in.BankBSB
	if bsb != "" {
		if !bsbPattern.MatchString(bsb) {
			return OrgSettings{}, fmt.Errorf("%w: invalid bank_bsb", ErrInvalidInput)
		}
		digits := strings.ReplaceAll(bsb, "-", "")
		bsb = digits[:3] + "-" + digits[3:]
	}
	account := in.BankAccount
	if account != "" && !accountPattern.MatchString(account) {
		return OrgSettings{}, fmt.Errorf("%w: invalid bank_account", ErrInvalidInput)
	}
	if (bsb == "") != (account == "") {
		return OrgSettings{}, fmt.Errorf("%w: bank_bsb and bank_account must both be set or both be empty", ErrInvalidInput)
	}

	if in.PayID != "" {
		if len(in.PayID) > 256 {
			return OrgSettings{}, fmt.Errorf("%w: invalid payid", ErrInvalidInput)
		}
		if !validPayID(in.PayID) {
			return OrgSettings{}, fmt.Errorf("%w: invalid payid", ErrInvalidInput)
		}
	}

	in.EmergencyPhone = strings.TrimSpace(in.EmergencyPhone)
	if in.EmergencyPhone != "" {
		digits := 0
		for _, r := range in.EmergencyPhone {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if !phonePattern.MatchString(in.EmergencyPhone) || digits < 6 {
			return OrgSettings{}, fmt.Errorf("%w: invalid emergency_phone", ErrInvalidInput)
		}
	}

	profile, err := validateProfile(in)
	if err != nil {
		return OrgSettings{}, err
	}
	in = profile

	if in.InvoiceLeadDays < 0 || in.InvoiceLeadDays > 14 {
		return OrgSettings{}, fmt.Errorf("%w: invalid invoice_lead_days", ErrInvalidInput)
	}

	out := in
	out.BankBSB = bsb
	out.BankAccount = account
	return out, nil
}

const settingsColumns = `name, coalesce(abn,''), gst_registered, coalesce(bas_frequency,''),
	coalesce(bank_bsb,''), coalesce(bank_account,''), coalesce(payid,''),
	invoice_lead_days, auto_send_rent, emergency_phone,
	address, phone, email, quote_terms, licences, logo_file_id`

func scanSettings(row pgx.Row) (OrgSettings, error) {
	var s OrgSettings
	var licences []byte
	err := row.Scan(&s.Name, &s.ABN, &s.GSTRegistered, &s.BASFrequency,
		&s.BankBSB, &s.BankAccount, &s.PayID, &s.InvoiceLeadDays, &s.AutoSendRent, &s.EmergencyPhone,
		&s.Address, &s.Phone, &s.Email, &s.QuoteTerms, &licences, &s.LogoFileID)
	if err != nil {
		return s, err
	}
	s.Licences = []Licence{}
	if err := json.Unmarshal(licences, &s.Licences); err != nil {
		return s, fmt.Errorf("decoding licences: %w", err)
	}
	return s, nil
}

// GetSettings looks up orgID's settings. It returns ErrNotFound if no such
// org exists.
func GetSettings(ctx context.Context, q Querier, orgID int64) (OrgSettings, error) {
	row := q.QueryRow(ctx, `SELECT `+settingsColumns+` FROM orgs WHERE id = $1`, orgID)
	s, err := scanSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgSettings{}, ErrNotFound
	}
	return s, err
}

// UpdateSettings validates in (returning ErrInvalidInput wrapped with the
// offending field's name on failure) and replaces orgID's settings
// wholesale. An empty optional field (abn, bank_bsb, bank_account, payid or
// bas_frequency) clears it (stored as SQL NULL), so billing.PayOptions
// keeps treating it as unset. It returns ErrNotFound if no such org exists.
func UpdateSettings(ctx context.Context, q Querier, orgID int64, in OrgSettings) (OrgSettings, error) {
	validated, err := validateSettings(in)
	if err != nil {
		return OrgSettings{}, err
	}
	licences, err := json.Marshal(validated.Licences)
	if err != nil {
		return OrgSettings{}, err
	}

	row := q.QueryRow(ctx, `
		UPDATE orgs SET
			name = $1,
			abn = NULLIF($2, ''),
			gst_registered = $3,
			bas_frequency = NULLIF($4, ''),
			bank_bsb = NULLIF($5, ''),
			bank_account = NULLIF($6, ''),
			payid = NULLIF($7, ''),
			invoice_lead_days = $8,
			auto_send_rent = $9,
			emergency_phone = $11,
			address = $12,
			phone = $13,
			email = $14,
			quote_terms = $15,
			licences = $16
		WHERE id = $10
		RETURNING `+settingsColumns,
		validated.Name, validated.ABN, validated.GSTRegistered, validated.BASFrequency,
		validated.BankBSB, validated.BankAccount, validated.PayID,
		validated.InvoiceLeadDays, validated.AutoSendRent, orgID, validated.EmergencyPhone,
		validated.Address, validated.Phone, validated.Email, validated.QuoteTerms, licences,
	)
	out, err := scanSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgSettings{}, ErrNotFound
	}
	return out, err
}

// validateProfile checks and trims the business profile fields.
func validateProfile(in OrgSettings) (OrgSettings, error) {
	in.Address = strings.TrimSpace(in.Address)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Email = strings.TrimSpace(in.Email)
	if utf8.RuneCountInString(in.Address) > maxAddress {
		return OrgSettings{}, fmt.Errorf("%w: address must be at most %d characters", ErrInvalidInput, maxAddress)
	}
	if in.Phone != "" && !phonePattern.MatchString(in.Phone) {
		return OrgSettings{}, fmt.Errorf("%w: invalid phone", ErrInvalidInput)
	}
	if in.Email != "" {
		addr, err := mail.ParseAddress(in.Email)
		if err != nil || addr.Address != in.Email {
			return OrgSettings{}, fmt.Errorf("%w: invalid email", ErrInvalidInput)
		}
	}
	if utf8.RuneCountInString(in.QuoteTerms) > maxQuoteTerms {
		return OrgSettings{}, fmt.Errorf("%w: quote_terms must be at most %d characters", ErrInvalidInput, maxQuoteTerms)
	}
	if len(in.Licences) > maxLicences {
		return OrgSettings{}, fmt.Errorf("%w: licences: at most %d", ErrInvalidInput, maxLicences)
	}
	licences := make([]Licence, len(in.Licences))
	for i, l := range in.Licences {
		l.Type, l.Number = strings.TrimSpace(l.Type), strings.TrimSpace(l.Number)
		if l.Type == "" || l.Number == "" {
			return OrgSettings{}, fmt.Errorf("%w: licences: type and number are required", ErrInvalidInput)
		}
		if utf8.RuneCountInString(l.Type) > maxLicenceField || utf8.RuneCountInString(l.Number) > maxLicenceField {
			return OrgSettings{}, fmt.Errorf("%w: licences: type and number must be at most %d characters", ErrInvalidInput, maxLicenceField)
		}
		licences[i] = l
	}
	in.Licences = licences
	return in, nil
}

// SetOrgLogo points orgID's logo at fileID (nil clears it) and returns the
// previous logo file, which the caller should delete if unreferenced.
func SetOrgLogo(ctx context.Context, q Querier, orgID int64, fileID *int64) (*int64, error) {
	var old *int64
	err := q.QueryRow(ctx, `SELECT logo_file_id FROM orgs WHERE id = $1 FOR UPDATE`, orgID).Scan(&old)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `UPDATE orgs SET logo_file_id = $2 WHERE id = $1`, orgID, fileID); err != nil {
		return nil, err
	}
	return old, nil
}
