package auth

import (
	"net/mail"
	"strings"
)

// IsValidEmail verifies whether the provided email string complies with standard
// email address syntax (RFC 5322), has a non-empty local-part and domain, and
// possesses a valid domain with at least one dot (e.g. user@example.com).
func IsValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}

	domain := parts[1]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if label == "" {
			return false
		}
	}

	return true
}

// IsValidDomain validates that a domain string (such as email_domain for university categories)
// is lowercase, contains no '@', contains at least one dot, and has valid labels.
func IsValidDomain(domain string) bool {
	domain = strings.TrimSpace(domain)
	if domain == "" || strings.ToLower(domain) != domain {
		return false
	}
	if strings.Contains(domain, "@") || strings.Contains(domain, " ") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return false
	}

	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if label == "" {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}

	return true
}
