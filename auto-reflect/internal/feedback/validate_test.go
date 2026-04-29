package feedback

import "testing"

func TestValidateAddInputRules(t *testing.T) {
	start := 2
	end := 4

	cases := []struct {
		name     string
		input    AddInput
		wantErrs bool
	}{
		{
			name: "happy path",
			input: AddInput{
				Kind:    "helpful",
				Comment: "ok",
				File:    "docs/x.md",
				Start:   &start,
				End:     &end,
			},
			wantErrs: false,
		},
		{
			name: "uppercase kind accepted",
			input: AddInput{
				Kind:    "HELPFUL",
				Comment: "ok",
			},
			wantErrs: false,
		},
		{
			name: "whitespace comment rejected",
			input: AddInput{
				Kind:    "helpful",
				Comment: "   ",
			},
			wantErrs: true,
		},
		{
			name: "end without start allowed",
			input: AddInput{
				Kind:    "helpful",
				Comment: "ok",
				File:    "docs/x.md",
				End:     &end,
			},
			wantErrs: false,
		},
		{
			name: "start without end allowed",
			input: AddInput{
				Kind:    "helpful",
				Comment: "ok",
				File:    "docs/x.md",
				Start:   &start,
			},
			wantErrs: false,
		},
		{
			name: "file required when end set",
			input: AddInput{
				Kind:    "helpful",
				Comment: "ok",
				End:     &end,
			},
			wantErrs: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateAddInput(&tc.input)
			if tc.wantErrs && len(errs) == 0 {
				t.Fatal("expected validation errors")
			}
			if !tc.wantErrs && len(errs) > 0 {
				t.Fatalf("unexpected validation errors: %#v", errs)
			}
		})
	}
}

func TestNormalizeRepoRelativePath(t *testing.T) {
	path, err := NormalizeRepoRelativePath(" docs/auth.md ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "docs/auth.md" {
		t.Fatalf("got %q", path)
	}

	if _, err := NormalizeRepoRelativePath("../outside"); err == nil {
		t.Fatal("expected parent traversal error")
	}
}
