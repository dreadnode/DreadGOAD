package labconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/jsonmerge"
)

// repoRoot walks up from the package directory to the checkout root, which is
// where ad/ and ansible/ live.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ad")); err != nil {
		t.Fatalf("repo root %s has no ad/ directory: %v", dir, err)
	}
	return dir
}

// testOptions builds Options with VulnsRequiringVars derived from the role
// sources, so adding a vulns_vars-consuming role automatically extends the
// pairing check instead of quietly falling outside it.
func testOptions(t *testing.T, root string) Options {
	t.Helper()
	opts := DefaultOptions()
	opts.VulnsRequiringVars = vulnsRequiringVars(t, root)
	if len(opts.VulnsRequiringVars) == 0 {
		t.Fatal("derived no vulns_vars-consuming roles; the role scan is broken")
	}
	return opts
}

func vulnsRequiringVars(t *testing.T, root string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "ansible", "roles"))
	if err != nil {
		t.Fatalf("read roles dir: %v", err)
	}

	out := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "vulns_") {
			continue
		}
		tasks := filepath.Join(root, "ansible", "roles", e.Name(), "tasks")
		if uses, err := treeMentions(tasks, "vulns_vars"); err == nil && uses {
			out[strings.TrimPrefix(e.Name(), "vulns_")] = true
		}
	}
	return out
}

func treeMentions(dir, needle string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), needle) {
			found = true
		}
		return nil
	})
	return found, err
}

// labDataDirs returns every ad/<lab>/data directory that holds a config.json.
func labDataDirs(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "ad", "*", "data", "config.json"))
	if err != nil {
		t.Fatalf("glob lab configs: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("found no ad/*/data/config.json")
	}
	dirs := make([]string, 0, len(matches))
	for _, m := range matches {
		dirs = append(dirs, filepath.Dir(m))
	}
	return dirs
}

var overlayRE = regexp.MustCompile(`^(.+)-overlay\.json$`)

// mergedVariants returns the config as each environment actually resolves it:
// the base alone (which is what an env with no overlay provisions) plus one
// merged document per {env}-overlay.json.
func mergedVariants(t *testing.T, dataDir string) map[string][]byte {
	t.Helper()
	base, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}

	out := map[string][]byte{"base (no overlay)": base}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("read %s: %v", dataDir, err)
	}
	for _, e := range entries {
		m := overlayRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		patch, err := os.ReadFile(filepath.Join(dataDir, e.Name()))
		if err != nil {
			t.Fatalf("read overlay %s: %v", e.Name(), err)
		}
		merged, err := jsonmerge.MergePatchBytes(base, patch)
		if err != nil {
			t.Fatalf("merge overlay %s: %v", e.Name(), err)
		}
		out[m[1]] = merged
	}
	return out
}

// baselinePath holds the findings that are known and accepted, so this gate can
// be green without pretending the lab data is clean. See the file's own header.
const baselinePath = "testdata/known_findings.txt"

// loadBaseline reads accepted findings keyed as "<lab>|<env>|<finding>".
func loadBaseline(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline %s: %v", baselinePath, err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out
}

// TestLabConfigIntegrity is the regression gate: every lab, in every
// environment, must reference only entities that survive the overlay merge.
//
// Findings listed in the baseline are reported as logs instead of failures.
// Anything new fails, and a baseline entry that stops occurring also fails, so
// the accepted set can only shrink.
func TestLabConfigIntegrity(t *testing.T) {
	root := repoRoot(t)
	opts := testOptions(t, root)
	baseline := loadBaseline(t)
	seen := map[string]bool{}

	for _, dataDir := range labDataDirs(t, root) {
		lab := filepath.Base(filepath.Dir(dataDir))
		t.Run(lab, func(t *testing.T) {
			base, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
			if err != nil {
				t.Fatalf("read base config: %v", err)
			}
			for env, merged := range mergedVariants(t, dataDir) {
				t.Run(env, func(t *testing.T) {
					findings, err := CheckIntegrity(merged, opts)
					if err != nil {
						t.Fatalf("CheckIntegrity: %v", err)
					}
					// Drops are only meaningful against the base, so this is a
					// no-op for the base pseudo-env and needs no special case.
					drops, err := CheckOverlayDrops(base, merged)
					if err != nil {
						t.Fatalf("CheckOverlayDrops: %v", err)
					}
					findings = append(findings, drops...)
					for _, f := range findings {
						key := lab + "|" + env + "|" + f.String()
						if baseline[key] {
							seen[key] = true
							t.Logf("known: %s", f)
							continue
						}
						t.Errorf("%s\n\tif this is intended, add to %s with a reason:\n\t%s",
							f, baselinePath, key)
					}
				})
			}
		})
	}

	for key := range baseline {
		if !seen[key] {
			t.Errorf("baseline entry no longer occurs, delete it from %s:\n\t%s", baselinePath, key)
		}
	}
}

// TestCheckIntegrityCatchesOverlayRegressions pins the three defects that
// shipped in ad/GOAD's per-env overlays, each expressed as the minimal merge
// patch that reintroduces it. These are the cases a base-config-only check
// cannot see.
func TestCheckIntegrityCatchesOverlayRegressions(t *testing.T) {
	root := repoRoot(t)
	opts := testOptions(t, root)
	base, err := os.ReadFile(filepath.Join(root, "ad", "GOAD", "data", "config.json"))
	if err != nil {
		t.Fatalf("read GOAD config: %v", err)
	}

	tests := []struct {
		name      string
		patch     string
		wantPath  string
		wantRef   string
		wantInMsg string
	}{
		{
			name: "overlay deletes the group ESC13 targets",
			// What dev/staging/test-overlay.json did: drop greatmaster while
			// leaving adcs_esc13 and its vulns_vars pointing at it.
			patch:     `{"lab":{"domains":{"essos.local":{"groups":{"universal":{"greatmaster":null}}}}}}`,
			wantPath:  `hosts["dc03"].vulns_vars.adcs_esc13.*.adcs_esc13_group`,
			wantRef:   "greatmaster",
			wantInMsg: "unresolved group",
		},
		{
			name:      "managed_by names an account the lab never creates",
			patch:     `{"lab":{"domains":{"essos.local":{"groups":{"global":{"Dragons":{"managed_by":"goadmin"}}}}}}}`,
			wantPath:  `domains["essos.local"].groups.global.Dragons.managed_by`,
			wantRef:   "goadmin",
			wantInMsg: "unresolved managed_by principal",
		},
		{
			name:      "overlay strips a vulns_vars entry the vuln still needs",
			patch:     `{"lab":{"domains":{},"hosts":{"dc03":{"vulns_vars":{"adcs_esc13":null}}}}}`,
			wantPath:  `hosts["dc03"].vulns`,
			wantRef:   "adcs_esc13",
			wantInMsg: "provisions nothing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := jsonmerge.MergePatchBytes(base, []byte(tc.patch))
			if err != nil {
				t.Fatalf("merge patch: %v", err)
			}
			findings, err := CheckIntegrity(merged, opts)
			if err != nil {
				t.Fatalf("CheckIntegrity: %v", err)
			}
			for _, f := range findings {
				if f.Path == tc.wantPath && f.Ref == tc.wantRef && strings.Contains(f.Msg, tc.wantInMsg) {
					return
				}
			}
			t.Errorf("no finding matched path=%q ref=%q msg~=%q; got %v",
				tc.wantPath, tc.wantRef, tc.wantInMsg, findings)
		})
	}
}

// TestCheckOverlayDropsCatchesSilentRemoval pins the defect that shipped on
// 2026-07-30: adcs_esc10_case1 was added to dc03 in config.json, but the dev,
// staging and test overlays each redeclared dc03.vulns without it, so the role
// never ran and ESC6/ESC9 stayed unexploitable in exactly those environments.
//
// The merged document is internally consistent in that state, which is why
// CheckIntegrity reports nothing and this check has to exist separately.
func TestCheckOverlayDropsCatchesSilentRemoval(t *testing.T) {
	root := repoRoot(t)
	base, err := os.ReadFile(filepath.Join(root, "ad", "GOAD", "data", "config.json"))
	if err != nil {
		t.Fatalf("read GOAD config: %v", err)
	}

	// Redeclare dc03.vulns without adcs_esc10_case1, exactly as the overlays did.
	patch := `{"lab":{"hosts":{"dc03":{"vulns":["ntlmdowngrade","disable_firewall","adcs_esc7","adcs_esc13","adcs_esc15"]}}}}`
	merged, err := jsonmerge.MergePatchBytes(base, []byte(patch))
	if err != nil {
		t.Fatalf("merge patch: %v", err)
	}

	if findings, err := CheckIntegrity(merged, testOptions(t, root)); err != nil {
		t.Fatalf("CheckIntegrity: %v", err)
	} else if len(findings) > 0 {
		t.Fatalf("CheckIntegrity unexpectedly reports the drop, so this test no longer\n"+
			"proves CheckOverlayDrops is load-bearing; got %v", findings)
	}

	drops, err := CheckOverlayDrops(base, merged)
	if err != nil {
		t.Fatalf("CheckOverlayDrops: %v", err)
	}
	for _, f := range drops {
		if f.Path == `hosts["dc03"].vulns` && f.Ref == "adcs_esc10_case1" {
			return
		}
	}
	t.Errorf(`no finding for hosts["dc03"].vulns ref=adcs_esc10_case1; got %v`, drops)
}

// TestCheckOverlayDropsIgnoresDeletedHost keeps the check quiet about removals
// that are already legible in the overlay: a host deleted outright with a null
// is an explicit choice, not a silent loss of capability.
func TestCheckOverlayDropsIgnoresDeletedHost(t *testing.T) {
	base := []byte(`{"lab":{"hosts":{"dc03":{"vulns":["adcs_esc10_case1"]}}}}`)
	merged, err := jsonmerge.MergePatchBytes(base, []byte(`{"lab":{"hosts":{"dc03":null}}}`))
	if err != nil {
		t.Fatalf("merge patch: %v", err)
	}
	drops, err := CheckOverlayDrops(base, merged)
	if err != nil {
		t.Fatalf("CheckOverlayDrops: %v", err)
	}
	if len(drops) != 0 {
		t.Errorf("deleting a host should report nothing; got %v", drops)
	}
}

// TestCheckIntegrityAcceptsBuiltinsAndCrossDomainRefs guards against the
// opposite failure: a validator noisy enough that people stop reading it.
func TestCheckIntegrityAcceptsBuiltinsAndCrossDomainRefs(t *testing.T) {
	doc := map[string]any{
		"lab": map[string]any{
			"domains": map[string]any{
				"a.local": map[string]any{
					"dc":                 "dc01",
					"netbios_name":       "A",
					"trust":              "b.local",
					"users":              map[string]any{"alice": map[string]any{"groups": []string{"Domain Admins", "Protected Users"}}},
					"groups":             map[string]any{"global": map[string]any{"Team": map[string]any{"managed_by": "Administrator", "members": []string{"B\\bob"}}}},
					"laps_readers":       []string{"alice"},
					"organisation_units": map[string]any{},
					"acls": map[string]any{
						"dn_target":    map[string]any{"for": "alice", "to": "CN=AdminSDHolder,CN=System,DC=a,DC=local", "right": "GenericAll"},
						"anon":         map[string]any{"for": "NT AUTHORITY\\ANONYMOUS LOGON", "to": "DC=a,DC=local", "right": "ReadProperty"},
						"machine":      map[string]any{"for": "alice", "to": "host01$", "right": "GenericAll"},
						"trust_acct":   map[string]any{"for": "B$", "to": "alice", "right": "GenericAll"},
						"cross_domain": map[string]any{"for": "bob", "to": "alice", "right": "GenericAll"},
					},
				},
				"b.local": map[string]any{
					"dc":           "dc02",
					"netbios_name": "B",
					"users":        map[string]any{"bob": map[string]any{}},
					"groups":       map[string]any{},
				},
			},
			"hosts": map[string]any{
				"dc01": map[string]any{"hostname": "host01", "domain": "a.local",
					"local_groups": map[string]any{"Administrators": []string{"A\\alice", "a\\Team"}}},
				"dc02": map[string]any{"hostname": "host02", "domain": "b.local"},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	findings, err := CheckIntegrity(raw, DefaultOptions())
	if err != nil {
		t.Fatalf("CheckIntegrity: %v", err)
	}
	for _, f := range findings {
		t.Errorf("false positive: %s", f)
	}
}
