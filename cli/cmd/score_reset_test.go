package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runHostsFilter executes the generated awk filters against a fixture
// /etc/hosts and returns (lines counted as attacker-seeded, lines kept by the
// rewrite). Exercising the real awk is the point: the regexes are where this
// feature can actually break, and a string match on the script would not
// notice a wrong one.
func runHostsFilter(t *testing.T, content string) (found string, kept string) {
	t.Helper()

	awkBin, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("awk not available")
	}

	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	countOut, err := exec.Command(awkBin, hostsFindProgram, path).Output()
	if err != nil {
		t.Fatalf("count filter: %v", err)
	}
	keptOut, err := exec.Command(awkBin, hostsKeepProgram, path).Output()
	if err != nil {
		t.Fatalf("keep filter: %v", err)
	}
	return strings.TrimSpace(string(countOut)), string(keptOut)
}

func TestHostsBaselineFilter(t *testing.T) {
	tests := []struct {
		name          string
		hosts         string
		wantFound     string
		wantKept      []string // substrings that must survive the rewrite
		wantGone      []string // substrings that must not
		wantKeptEmpty bool     // nothing survives, so the rewrite must be refused
	}{
		{
			name: "pristine kali plus azure cloud-init",
			hosts: "127.0.0.1\tlocalhost\n" +
				"::1\t\tlocalhost ip6-localhost ip6-loopback\n" +
				"fe00::0\t\tip6-localnet\n" +
				"ff00::0\t\tip6-mcastprefix\n" +
				"ff02::1\t\tip6-allnodes\n" +
				"ff02::2\t\tip6-allrouters\n" +
				"\n" +
				"127.0.1.1\tkali\n" +
				"127.0.0.1 kali-attack-box\n",
			wantFound: "0",
			wantKept:  []string{"127.0.0.1\tlocalhost", "ff02::2", "127.0.1.1\tkali", "kali-attack-box"},
		},
		{
			name: "nxc --generate-hosts-file output appended",
			hosts: "127.0.0.1\tlocalhost\n" +
				"::1\t\tlocalhost ip6-localhost ip6-loopback\n" +
				"127.0.1.1\tkali\n" +
				"10.10.10.10     dc01.sevenkingdoms.local dc01\n" +
				"10.10.10.11     castelblack.north.sevenkingdoms.local castelblack\n",
			wantFound: "2",
			wantKept:  []string{"127.0.0.1", "::1", "127.0.1.1"},
			wantGone:  []string{"dc01.sevenkingdoms.local", "castelblack"},
		},
		{
			name: "uppercase IPv6 reserved rows are baseline, not artifacts",
			hosts: "127.0.0.1 localhost\n" +
				"FE80::1 link-local\n" +
				"FF02::1 ip6-allnodes\n",
			wantFound: "0",
			wantKept:  []string{"FE80::1", "FF02::1"},
		},
		{
			name: "unique-local and other routable v6 are artifacts",
			hosts: "127.0.0.1 localhost\n" +
				"fd00::5 evil.corp.local\n" +
				"2001:db8::1 srv02.corp.local\n",
			wantFound: "2",
			wantKept:  []string{"127.0.0.1 localhost"},
			wantGone:  []string{"evil.corp.local", "srv02.corp.local"},
		},
		{
			// Commenting an entry out hides it from resolution but not from
			// the next agent reading the file.
			name: "commented-out entries are artifacts",
			hosts: "127.0.0.1 localhost\n" +
				"# 10.10.10.12   dc02.sevenkingdoms.local dc02\n" +
				"#10.10.10.13 srv03.sevenkingdoms.local\n" +
				"##  10.10.10.14 srv04.sevenkingdoms.local\n",
			wantFound: "3",
			wantKept:  []string{"127.0.0.1 localhost"},
			wantGone:  []string{"dc02", "srv03", "srv04"},
		},
		{
			// Prose comments are baseline: cloud-init writes this block on
			// Azure images and stripping it would not restore pristine.
			name: "prose comments survive",
			hosts: "# Your system has configured 'manage_etc_hosts' as True.\n" +
				"# As a result, if you wish for changes to this file to persist\n" +
				"# then you will need to either:\n" +
				"# a.) make changes to the master file in /etc/cloud/templates/\n" +
				"# The following lines are desirable for IPv6 capable hosts\n" +
				"127.0.0.1 localhost\n",
			wantFound: "0",
			wantKept:  []string{"manage_etc_hosts", "IPv6 capable hosts", "a.) make changes"},
		},
		{
			// Splitting an indented line without stripping the leading
			// whitespace yields an empty first field, which would read as
			// prose and let the entry through.
			name:      "indented entries are still artifacts",
			hosts:     "127.0.0.1 localhost\n  \t10.10.10.15 srv05.sevenkingdoms.local\n",
			wantFound: "1",
			wantKept:  []string{"127.0.0.1 localhost"},
			wantGone:  []string{"srv05"},
		},
		{
			// A commented loopback line leaks nothing.
			name:      "commented loopback stays baseline",
			hosts:     "127.0.0.1 localhost\n# 127.0.1.1 oldkali\n# ::1 localhost\n",
			wantFound: "0",
			wantKept:  []string{"oldkali", "# ::1 localhost"},
		},
		{
			// An agent that clobbered /etc/hosts outright
			// (`nxc --generate-hosts-file > /etc/hosts`) leaves nothing for the
			// filter to keep. Installing that empty result would break hostname
			// resolution, so the clean script's loopback guard must refuse it.
			name:          "clobbered hosts file leaves nothing to keep",
			hosts:         "10.10.10.10 dc01.local dc01\n10.10.10.11 srv02.local\n",
			wantFound:     "2",
			wantGone:      []string{"dc01", "srv02"},
			wantKeptEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, kept := runHostsFilter(t, tt.hosts)
			if found != tt.wantFound {
				t.Errorf("found = %s, want %s (kept:\n%s)", found, tt.wantFound, kept)
			}
			for _, want := range tt.wantKept {
				if !strings.Contains(kept, want) {
					t.Errorf("rewrite dropped baseline line %q (kept:\n%s)", want, kept)
				}
			}
			for _, gone := range tt.wantGone {
				if strings.Contains(kept, gone) {
					t.Errorf("rewrite preserved attacker entry %q (kept:\n%s)", gone, kept)
				}
			}
			if tt.wantKeptEmpty && strings.TrimSpace(kept) != "" {
				t.Errorf("expected nothing to survive the filter, got:\n%s", kept)
			}
		})
	}
}

// TestHostsLoopbackGuard runs the real guard against real filter output. The
// rewrite is refused unless baseline survived, and either loopback family
// counts as baseline — a file whose only loopback is `::1` still resolves
// localhost, so skipping the scrub there would leave artifacts behind.
func TestHostsLoopbackGuard(t *testing.T) {
	grepBin, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("grep not available")
	}

	tests := []struct {
		name       string
		hosts      string
		wantAccept bool
	}{
		{
			name:       "IPv4 loopback survives",
			hosts:      "127.0.0.1 localhost\n10.10.10.10 dc01.local\n",
			wantAccept: true,
		},
		{
			name:       "IPv6-only baseline survives",
			hosts:      "::1 localhost ip6-localhost\nff02::1 ip6-allnodes\n10.10.10.10 dc01.local\n",
			wantAccept: true,
		},
		{
			name:       "clobbered file leaves no loopback",
			hosts:      "10.10.10.10 dc01.local\n10.10.10.11 srv02.local\n",
			wantAccept: false,
		},
		{
			name:       "comments alone are not a loopback line",
			hosts:      "# 127.0.1.1 oldkali\n# prose\n10.10.10.10 dc01.local\n",
			wantAccept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, kept := runHostsFilter(t, tt.hosts)

			path := filepath.Join(t.TempDir(), "filtered")
			if err := os.WriteFile(path, []byte(kept), 0o644); err != nil {
				t.Fatalf("write filtered: %v", err)
			}

			err := exec.Command(grepBin, "-qE", hostsLoopbackGuard, path).Run()
			if accepted := err == nil; accepted != tt.wantAccept {
				t.Errorf("guard accepted = %v, want %v (filtered:\n%s)", accepted, tt.wantAccept, kept)
			}
		})
	}
}

// TestKaliCleanupScriptStripsAttackerHosts pins the script shape: both modes
// inspect /etc/hosts, apply mode rewrites it through a mktemp file under
// non-prompting sudo, and dry-run never mutates it.
func TestKaliCleanupScriptStripsAttackerHosts(t *testing.T) {
	for _, apply := range []bool{false, true} {
		if !strings.Contains(buildKaliCleanupScript(apply), "/etc/hosts") {
			t.Fatalf("apply=%v: cleanup script does not reference /etc/hosts", apply)
		}
	}

	applyScript := buildKaliCleanupScript(true)
	for _, want := range []string{
		`dg_t=$(mktemp 2>/dev/null)`, // not a fixed /tmp path root copies from
		`awk "$dg_hosts_keep" /etc/hosts > "$dg_t"`,
		`sudo -n cp "$dg_t" /etc/hosts`,
		`grep -qE '` + hostsLoopbackGuard + `'`,
	} {
		if !strings.Contains(applyScript, want) {
			t.Errorf("apply mode missing %q", want)
		}
	}
	if strings.Contains(applyScript, "/tmp/.dg_hosts") {
		t.Error("apply mode must not stage the rewrite at a predictable /tmp path")
	}

	// Dry-run must be side-effect free: it may count, never rewrite.
	if strings.Contains(buildKaliCleanupScript(false), "sudo -n cp") {
		t.Error("dry-run must not contain the /etc/hosts rewrite command")
	}
}

// TestKaliCleanupScriptIsValidShell catches quoting or syntax damage in the
// generated script without executing any of it.
func TestKaliCleanupScriptIsValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	for _, apply := range []bool{false, true} {
		cmd := exec.Command(sh, "-n")
		cmd.Stdin = strings.NewReader(buildKaliCleanupScript(apply))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("apply=%v: generated script is not valid sh: %v\n%s", apply, err, out)
		}
	}
}
