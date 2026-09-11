package redact

import "testing"

func TestSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{name: "empty", secret: "", want: "***"},
		{name: "short", secret: "abcd", want: "***"},
		{name: "long", secret: "AKIAIOSFODNN7EXAMPLE", want: "****MPLE"}, //nolint:gosec // test fixture, not a real key.
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Secret(test.secret)
			if got != test.want {
				t.Errorf("Secret(%q) = %q, want %q", test.secret, got, test.want)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "no userinfo",
			raw:  "https://example.com/file.bin",
			want: "https://example.com/file.bin",
		},
		{ //nolint:gosec // test fixture, not a real credential.
			name: "user and password",
			raw:  "https://user:sup3rsecret@example.com/file.bin",
			want: "https://****cret@example.com/file.bin",
		},
		{
			name: "user only",
			raw:  "https://someuser@example.com/file.bin",
			want: "https://****user@example.com/file.bin",
		},
		{ //nolint:gosec // test fixture, not a real credential.
			name: "unsupported scheme keeps redaction",
			raw:  "ftp://user:sup3rsecret@example.com/file.bin",
			want: "ftp://****cret@example.com/file.bin",
		},
		{
			name: "s3 url",
			raw:  "s3://alias/path/to/file.bin",
			want: "s3://alias/path/to/file.bin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := URL(test.raw)
			if got != test.want {
				t.Errorf("URL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

// A URL that cannot be parsed must never be echoed back with its credentials
// intact, so make sure the parse failure path is exercised.
func TestURLUnparseable(t *testing.T) {
	raw := "https://user:pass@exa mple.com/\x7f" //nolint:gosec // test fixture, not a real credential.

	got := URL(raw)
	if got != raw {
		t.Errorf("URL(%q) = %q, want the input unchanged", raw, got)
	}
}
