package skill

import "testing"

func TestValidateVersionSpec(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"latest", "latest", false},
		{"branch", "branch:main", false},
		{"tag", "tag:v1.0", false},
		{"commit short", "commit:abcdef1", false},
		{"commit long", "commit:abcdef1234567", false},
		{"bare string", "some-tag", false},
		{"trimmed latest", "  latest  ", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"branch empty payload", "branch:", true},
		{"tag empty payload", "tag:", true},
		{"commit non-hex", "commit:xyz", true},
		{"commit too short", "commit:abc", true},
		{"unknown prefix", "foo:bar", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ve := ValidateVersionSpec(tc.input)
			if tc.wantErr && ve == nil {
				t.Fatalf("ValidateVersionSpec(%q) = nil, want error", tc.input)
			}
			if !tc.wantErr && ve != nil {
				t.Fatalf("ValidateVersionSpec(%q) = %+v, want nil", tc.input, ve)
			}
			if tc.wantErr && ve != nil && ve.Code != CodeInvalidVersionSpec {
				t.Errorf("ValidateVersionSpec(%q) code = %q, want %q", tc.input, ve.Code, CodeInvalidVersionSpec)
			}
		})
	}
}
