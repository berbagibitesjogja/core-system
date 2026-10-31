package mailer

import (
	"strings"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/config"
)

func TestRenderInvitationEmail(t *testing.T) {
	m := New(config.SMTPConfig{})

	tests := []struct {
		name     string
		data     InvitationEmailData
		contains []string
	}{
		{
			name: "renders invitation email with all fields",
			data: InvitationEmailData{
				RecipientName: "Ahmad",
				InviterName:   "Tim IT",
				Role:          "core",
				DivisionName:  "Managerial",
				InviteLink:    "https://app.berbagibitesjogja.com/register?token=test-token-1234",
				ExpiryHours:   48,
			},
			contains: []string{
				"Ahmad",
				"Tim IT",
				"core",
				"Managerial",
				"https://app.berbagibitesjogja.com/register?token=test-token-1234",
				"48 hours",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := m.RenderInvitationEmail(tc.data)
			if err != nil {
				t.Fatalf("unexpected rendering error: %v", err)
			}

			for _, expectedSubstr := range tc.contains {
				if !strings.Contains(rendered, expectedSubstr) {
					t.Fatalf("expected rendered email to contain %q, but got:\n%s", expectedSubstr, rendered)
				}
			}
		})
	}
}
