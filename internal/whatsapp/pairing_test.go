package whatsapp

import "testing"

func TestSanitizePhone(t *testing.T) {
	cases := []struct {
		name    string
		cc      string
		num     string
		want    string
		wantErr bool
	}{
		{name: "standard", cc: "+91", num: "98765 43210", want: "919876543210"},
		{name: "leading zero", cc: "44", num: "07911123456", want: "447911123456"},
		{name: "duplicate country code", cc: "91", num: "919876543210", want: "919876543210"},
		{name: "us number", cc: "1", num: "(555) 010-0199", want: "15550100199"},
		{name: "missing digits", cc: "91", num: "abc", wantErr: true},
		{name: "too short", cc: "1", num: "1234", wantErr: true},
		{name: "too long", cc: "91", num: "123456789012345", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizePhone(tc.cc, tc.num)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
