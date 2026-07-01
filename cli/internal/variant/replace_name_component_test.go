package variant

import "testing"

// TestReplaceNameComponent locks in the CamelCase-aware boundary matching that
// closes the compound-token leak (e.g. the GPO name "StarkWallpaper"). A name
// component is replaced when it is preceded by a word boundary and followed by
// end-of-string, a non-word character, or an UPPERCASE letter (CamelCase); it is
// left intact when followed by a lowercase letter, digit, or underscore.
func TestReplaceNameComponent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		old  string
		repl string
		want string
	}{
		{"camelCase boundary (the StarkWallpaper bug)", `New-GPO -Name "StarkWallpaper"`, "Stark", "Research", `New-GPO -Name "ResearchWallpaper"`},
		{"trailing backslash is a boundary", `north\Stark`, "Stark", "Research", `north\Research`},
		{"trailing dot is a boundary", "Stark.txt", "Stark", "Research", "Research.txt"},
		{"end of string", "Stark", "Stark", "Research", "Research"},
		{"standalone word", "the Stark house", "Stark", "Research", "the Research house"},
		{"multiple occurrences, mixed boundaries", "Stark and StarkWallpaper", "Stark", "Research", "Research and ResearchWallpaper"},
		{"lowercase continuation is NOT replaced", "Starkey", "Stark", "Research", "Starkey"},
		{"digit continuation is NOT replaced", "Stark1", "Stark", "Research", "Stark1"},
		{"underscore continuation is NOT replaced", "Stark_svc", "Stark", "Research", "Stark_svc"},
		{"no left boundary is NOT replaced", "aStark", "Stark", "Research", "aStark"},
		{"no occurrence is unchanged", "nothing here", "Stark", "Research", "nothing here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replaceNameComponent(tc.in, tc.old, tc.repl); got != tc.want {
				t.Errorf("replaceNameComponent(%q, %q, %q) = %q, want %q", tc.in, tc.old, tc.repl, got, tc.want)
			}
		})
	}
}
