package validate

import "testing"

func TestRenderScript_PSQuoteEscapesSingleQuotes(t *testing.T) {
	tmpl := `$X = {{psq .Name}}`
	got, err := renderScript(tmpl, map[string]any{"Name": "o'malley"})
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}
	want := `$X = 'o''malley'`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderScript_PSArr(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, "@()"},
		{"single", []string{"KB1"}, "@('KB1')"},
		{"many", []string{"KB1", "KB2"}, "@('KB1', 'KB2')"},
		{"escapes quotes", []string{"o'malley"}, "@('o''malley')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderScript(`{{psarr .Items}}`, map[string]any{"Items": tt.in})
			if err != nil {
				t.Fatalf("renderScript: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderScript_MissingVarErrors(t *testing.T) {
	// text/template rejects a missing map key when the consuming func has a
	// typed signature. That gives us strict-mode-by-default: a check that
	// forgets to pass a required var fails loudly instead of producing an
	// empty PowerShell literal that silently does the wrong thing.
	if _, err := renderScript(`$X = {{psq .Missing}}`, map[string]any{}); err == nil {
		t.Fatal("expected error for missing template var, got nil")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "happy path",
			raw:  "banner\n===BEGIN_JSON===\n{\"x\":1}\n===END_JSON===\n",
			want: `{"x":1}`,
		},
		{
			name: "trims surrounding whitespace",
			raw:  "===BEGIN_JSON===\n   {\"x\":1}   \n===END_JSON===",
			want: `{"x":1}`,
		},
		{
			name: "warning text before payload is tolerated",
			raw:  "WARNING: something\n===BEGIN_JSON==={\"x\":1}===END_JSON===",
			want: `{"x":1}`,
		},
		{name: "no markers", raw: `{"x":1}`, wantErr: true},
		{name: "missing end marker", raw: "===BEGIN_JSON===\n{\"x\":1}\n", wantErr: true},
		{name: "end before begin", raw: "===END_JSON===\n{\"x\":1}\n===BEGIN_JSON===", wantErr: true},
		{name: "empty payload", raw: "===BEGIN_JSON===\n   \n===END_JSON===", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSON(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
