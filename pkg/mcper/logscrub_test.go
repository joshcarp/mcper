package mcper

import (
	"strings"
	"testing"
)

func TestScrubBearerTokens(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"Authorization header",
			"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc123",
			"Authorization: <REDACTED>",
		},
		{
			"X-MCPER-Cap header",
			"x-mcper-cap: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc123",
			"x-mcper-cap: <REDACTED>",
		},
		{
			"mcper_cap JSON",
			`{"mcper_cap":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc"}`,
			`{"mcper_cap":<REDACTED>}`,
		},
		{
			"env var leak",
			`MCPER_CAP=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc`,
			`MCPER_CAP=<REDACTED>`,
		},
		{
			"raw JWT in body",
			`token is eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abcdefghijklmnop`,
			`token is <REDACTED>`,
		},
		{
			"no secrets",
			"plugin loaded ok",
			"plugin loaded ok",
		},
	}
	for _, c := range cases {
		got := ScrubBearerTokens(c.in)
		if !strings.Contains(got, "<REDACTED>") && c.want != c.in {
			t.Errorf("%s: expected redaction in %q, got %q", c.name, c.in, got)
			continue
		}
		// Ensure NO original JWT-looking substring survives.
		if strings.Contains(got, "eyJzdWIiOiJ4In0") {
			t.Errorf("%s: JWT payload leaked: %q", c.name, got)
		}
	}
}
