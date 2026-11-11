package auth

import (
	"testing"
)

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"", false},
		{"   ", false},
		{"@", false},
		{"@domain.com", false},
		{"user@", false},
		{"user@domain", false},
		{"user@domain.", false},
		{".user@domain.com", false},
		{"user..name@domain.com", false},
		{"user@.domain.com", false},
		{"Name <user@domain.com>", false},
		{"user@domain..com", false},
		{"user@domain.com", true},
		{"USER@DOMAIN.COM", true},
		{"user.name+tag@sub.domain.co.id", true},
		{"hero@mail.ugm.ac.id", true},
	}

	for _, tt := range tests {
		got := IsValidEmail(tt.email)
		if got != tt.valid {
			t.Errorf("IsValidEmail(%q) = %v; want %v", tt.email, got, tt.valid)
		}
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		domain string
		valid  bool
	}{
		{"", false},
		{"UGM.AC.ID", false}, // must be lowercase
		{"ugm", false},       // must have dot
		{".ugm.ac.id", false},
		{"ugm.ac.id.", false},
		{"user@ugm.ac.id", false}, // cannot have @
		{"ugm..ac.id", false},
		{"ugm ac id", false},
		{"ugm.ac.id", true},
		{"mail.ugm.ac.id", true},
		{"uii.ac.id", true},
		{"sub-domain.example.com", true},
	}

	for _, tt := range tests {
		got := IsValidDomain(tt.domain)
		if got != tt.valid {
			t.Errorf("IsValidDomain(%q) = %v; want %v", tt.domain, got, tt.valid)
		}
	}
}
