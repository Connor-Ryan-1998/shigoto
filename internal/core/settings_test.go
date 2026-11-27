package core

import (
	"errors"
	"strings"
	"testing"
)

func TestValidABN(t *testing.T) {
	cases := []struct {
		name string
		abn  string
		want bool
	}{
		{"valid", "51 824 753 556", true},
		{"bad checksum", "51 824 753 557", false},
		{"too short", "5182475355", false},
		{"empty allowed", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidABN(c.abn); got != c.want {
				t.Errorf("ValidABN(%q) = %v, want %v", c.abn, got, c.want)
			}
		})
	}
}

func TestSettingsValidation(t *testing.T) {
	base := func() OrgSettings {
		return OrgSettings{Name: "Acme", InvoiceLeadDays: 7}
	}

	cases := []struct {
		name    string
		modify  func(OrgSettings) OrgSettings
		wantErr bool
	}{
		{
			name:    "bad bsb format",
			modify:  func(s OrgSettings) OrgSettings { s.BankBSB = "12-3456"; s.BankAccount = "12345678"; return s },
			wantErr: true,
		},
		{
			name:    "bsb without account",
			modify:  func(s OrgSettings) OrgSettings { s.BankBSB = "123-456"; return s },
			wantErr: true,
		},
		{
			name:    "payid mobile with space passes",
			modify:  func(s OrgSettings) OrgSettings { s.PayID = "0412 345 678"; return s },
			wantErr: false,
		},
		{
			name:    "payid garbage fails",
			modify:  func(s OrgSettings) OrgSettings { s.PayID = "bob@"; return s },
			wantErr: true,
		},
		{
			name:    "emergency phone mobile",
			modify:  func(s OrgSettings) OrgSettings { s.EmergencyPhone = "0400 123 456"; return s },
			wantErr: false,
		},
		{
			name:    "emergency phone international landline",
			modify:  func(s OrgSettings) OrgSettings { s.EmergencyPhone = "+61 (7) 3000-0000"; return s },
			wantErr: false,
		},
		{
			name:    "emergency phone words",
			modify:  func(s OrgSettings) OrgSettings { s.EmergencyPhone = "call me"; return s },
			wantErr: true,
		},
		{
			name:    "emergency phone too short",
			modify:  func(s OrgSettings) OrgSettings { s.EmergencyPhone = "000"; return s },
			wantErr: true,
		},
		{
			name:    "invoice lead days too high",
			modify:  func(s OrgSettings) OrgSettings { s.InvoiceLeadDays = 15; return s },
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateSettings(c.modify(base()))
			if c.wantErr && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("validateSettings: err = %v, want ErrInvalidInput", err)
			}
			if !c.wantErr && err != nil {
				t.Errorf("validateSettings: err = %v, want nil", err)
			}
		})
	}
}

func TestProfileValidation(t *testing.T) {
	base := func() OrgSettings { return OrgSettings{Name: "Acme", InvoiceLeadDays: 7} }
	long := func(n int) string { return strings.Repeat("a", n) }
	five := []Licence{{"A", "1"}, {"B", "2"}, {"C", "3"}, {"D", "4"}, {"E", "5"}}
	cases := []struct {
		name    string
		modify  func(*OrgSettings)
		wantErr string
	}{
		{"full profile", func(s *OrgSettings) {
			s.Address, s.Phone, s.Email, s.QuoteTerms = "1 Main St\nBrisbane QLD 4000", "07 3000 1234", "hi@acme.test", "50% deposit"
			s.Licences = []Licence{{Type: "QBCC", Number: "1234567"}, {Type: "Electrical", Number: "98765"}}
		}, ""},
		{"five licences", func(s *OrgSettings) { s.Licences = five }, ""},
		{"six licences", func(s *OrgSettings) { s.Licences = append(append([]Licence{}, five...), Licence{"F", "6"}) }, "invalid input: licences: at most 5"},
		{"blank licence type", func(s *OrgSettings) { s.Licences = []Licence{{Type: " ", Number: "1"}} }, "invalid input: licences: type and number are required"},
		{"blank licence number", func(s *OrgSettings) { s.Licences = []Licence{{Type: "QBCC", Number: ""}} }, "invalid input: licences: type and number are required"},
		{"long licence type", func(s *OrgSettings) { s.Licences = []Licence{{Type: long(61), Number: "1"}} }, "invalid input: licences: type and number must be at most 60 characters"},
		{"bad email", func(s *OrgSettings) { s.Email = "not an email" }, "invalid input: invalid email"},
		{"bad phone", func(s *OrgSettings) { s.Phone = "call me" }, "invalid input: invalid phone"},
		{"long address", func(s *OrgSettings) { s.Address = long(301) }, "invalid input: address must be at most 300 characters"},
		{"long terms", func(s *OrgSettings) { s.QuoteTerms = long(4001) }, "invalid input: quote_terms must be at most 4000 characters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base()
			c.modify(&s)
			_, err := validateSettings(s)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != c.wantErr {
				t.Fatalf("validateSettings err = %q, want %q", got, c.wantErr)
			}
		})
	}
}

func TestProfileTrimsLicences(t *testing.T) {
	s := OrgSettings{Name: "Acme", InvoiceLeadDays: 7, Licences: []Licence{{Type: " QBCC ", Number: " 123 "}}}
	out, err := validateSettings(s)
	if err != nil || out.Licences[0] != (Licence{Type: "QBCC", Number: "123"}) {
		t.Fatalf("validateSettings = %+v, %v", out.Licences, err)
	}
}
