package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The mapping is the operator's claim about which legacy version belonged to
// which source, and it is the only input baseline cannot check against
// anything. So the parser's job is to reject everything it does not
// understand: a --map entry that were silently dropped would leave the
// versions it named unassigned, and the conversion would either be refused
// with a message about the wrong thing or — with the rest of the line still
// covering the history — record less than the operator wrote.

func TestParseMappingReadsRangesAndLists(t *testing.T) {
	cases := map[string]struct {
		values []string
		want   map[string][]int64
	}{
		"a range": {
			values: []string{"core:1-16"},
			want:   map[string][]int64{"core": versionsUpTo(16)},
		},
		"a list": {
			values: []string{"core:1,2,3"},
			want:   map[string][]int64{"core": {1, 2, 3}},
		},
		"both at once": {
			values: []string{"core:1-3,7,9-10"},
			want:   map[string][]int64{"core": {1, 2, 3, 7, 9, 10}},
		},
		"a range of one": {
			values: []string{"core:5-5"},
			want:   map[string][]int64{"core": {5}},
		},
		"two sources": {
			values: []string{"core:1-16", "orders:17,19-20"},
			want: map[string][]int64{
				"core":   versionsUpTo(16),
				"orders": {17, 19, 20},
			},
		},
		"spaces around the parts": {
			values: []string{" core : 1 - 3 , 5 "},
			want:   map[string][]int64{"core": {1, 2, 3, 5}},
		},
		"out of order": {
			values: []string{"core:3,1-2"},
			want:   map[string][]int64{"core": {3, 1, 2}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseMapping(tc.values)
			if err != nil {
				t.Fatalf("parsing %v failed: %v", tc.values, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parsing %v gave %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}

func TestParseMappingRefusesMalformedInput(t *testing.T) {
	cases := map[string]struct {
		values []string
		want   string
	}{
		"no colon":                {values: []string{"core"}, want: "has no colon"},
		"no source":               {values: []string{":1-16"}, want: "names no source"},
		"no versions":             {values: []string{"core:"}, want: "assigns no versions"},
		"blank versions":          {values: []string{"core:   "}, want: "assigns no versions"},
		"empty list entry":        {values: []string{"core:1,,3"}, want: "empty entry"},
		"trailing comma":          {values: []string{"core:1,"}, want: "empty entry"},
		"leading comma":           {values: []string{"core:,1"}, want: "empty entry"},
		"not a number":            {values: []string{"core:one"}, want: `"one", which is not a version number`},
		"not a number in a range": {values: []string{"core:1-x"}, want: `"x", which is not a version number`},
		"a reversed range":        {values: []string{"core:16-1"}, want: "counts down"},
		"version zero":            {values: []string{"core:0-3"}, want: "not above zero"},
		"a negative version":      {values: []string{"core:-5"}, want: "missing an end"},
		"a range with no end":     {values: []string{"core:5-"}, want: "missing an end"},
		"two dashes":              {values: []string{"core:1-2-3"}, want: "more than one dash"},
		"a duplicate source":      {values: []string{"core:1-3", "core:4"}, want: `names "core" twice`},
		"a duplicate version":     {values: []string{"core:1-3,2"}, want: "lists version 2 twice"},
		"a hex version":           {values: []string{"core:0x10"}, want: "not a version number"},
		// A mistyped range is the one malformed input that is expensive rather
		// than merely wrong: expanded, it is an allocation the size of the
		// typo.
		"an implausible range": {values: []string{"core:1-99999999"}, want: "not that long"},
		"an unparseable size":  {values: []string{"core:99999999999999999999"}, want: "not a version number"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mapping, err := parseMapping(tc.values)
			if err == nil {
				t.Fatalf("parsing %v should have been refused, got %v", tc.values, mapping)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error should name what was wrong; want it to mention %q, got: %v", tc.want, err)
			}
			// The offending text has to appear, or the operator is left
			// hunting for which of several --map flags was meant.
			if !strings.Contains(err.Error(), "--map") {
				t.Fatalf("the error should name the flag it came from, got: %v", err)
			}
		})
	}
}

func TestParseSourceDirsReadsNameAndPath(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()

	dirs, err := parseSourceDirs([]string{"orders=" + first, " billing = " + second + " "})
	if err != nil {
		t.Fatalf("parsing the source directories failed: %v", err)
	}

	want := []consumerSource{{name: "orders", path: first}, {name: "billing", path: second}}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("got %v, want %v", dirs, want)
	}
}

func TestParseSourceDirsRefusesMalformedInput(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "not-here")

	file := filepath.Join(dir, "00001_a.sql")
	if err := os.WriteFile(file, []byte("-- +goose Up\nSELECT 1;\n"), 0o600); err != nil {
		t.Fatalf("failed to write the fixture: %v", err)
	}

	cases := map[string]struct {
		values []string
		want   string
	}{
		"no equals":         {values: []string{"orders"}, want: `has no "="`},
		"no name":           {values: []string{"=" + dir}, want: "names no source"},
		"no path":           {values: []string{"orders="}, want: "gives no path"},
		"core":              {values: []string{"core=" + dir}, want: "embedded in this binary"},
		"a duplicate name":  {values: []string{"orders=" + dir, "orders=" + dir}, want: `names "orders" twice`},
		"a missing path":    {values: []string{"orders=" + missing}, want: missing},
		"a file not a path": {values: []string{"orders=" + file}, want: "is not a directory"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dirs, err := parseSourceDirs(tc.values)
			if err == nil {
				t.Fatalf("parsing %v should have been refused, got %v", tc.values, dirs)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error should name what was wrong; want it to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

// A source has to be supplied before it can be baselined, because a history
// table is written only for a source the runner registers. --unverified-source
// relaxes what the files are checked for, not whether they are needed, and an
// operator who expects otherwise has to be told so here rather than by a
// refusal about an unknown source.
func TestBaselineFlagsRefuseAPlanThatCannotBeCarriedOut(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]struct {
		argv []string
		want string
	}{
		"no mapping at all": {
			argv: []string{"baseline"},
			want: "needs at least one --map",
		},
		"a mapped source with no directory": {
			argv: []string{"baseline", "--map", "core:1-16", "--map", "orders:17"},
			want: "--source-dir orders=",
		},
		"a mapped source declared unverified instead of supplied": {
			argv: []string{"baseline", "--map", "core:1-16", "--map", "orders:17", "--unverified-source", "orders"},
			want: "--source-dir orders=",
		},
		"an unverified source that is not mapped": {
			argv: []string{"baseline", "--map", "core:1-16", "--unverified-source", "orders"},
			want: "has no --map",
		},
		"core declared unverified": {
			argv: []string{"baseline", "--map", "core:1-16", "--unverified-source", "core"},
			want: "embedded in this binary",
		},
		"an unnamed unverified source": {
			argv: []string{"baseline", "--map", "core:1-16", "--unverified-source", " "},
			want: "names no source",
		},
		"a source directory that is not there": {
			argv: []string{"baseline", "--map", "core:1-16", "--source-dir", "orders=" + filepath.Join(dir, "gone")},
			want: "--source-dir orders=",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts, err := parseArgs(tc.argv)
			if err == nil {
				t.Fatalf("%v should have been refused, got %+v", tc.argv, opts.baseline)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error should say what to do; want it to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

// --apply reads as consent to write. A command that does not read it must say
// so rather than run, having quietly discarded the one flag the operator was
// most deliberate about.
func TestBaselineFlagsAreRefusedOnOtherCommands(t *testing.T) {
	for _, argv := range [][]string{
		{"up", "--apply"},
		{"status", "--map", "core:1-16"},
		{"reset", "--force"},
	} {
		_, err := parseArgs(argv)
		if err == nil {
			t.Fatalf("%v should have been refused", argv)
		}
		if !strings.Contains(err.Error(), "baseline") {
			t.Fatalf("the error should name where the flag belongs, got: %v", err)
		}
	}
}

func TestParseArgsBuildsAWholeBaselinePlan(t *testing.T) {
	dir := t.TempDir()

	opts, err := parseArgs([]string{
		"baseline",
		"--map", "core:1-16",
		"--map", "orders:17,19-20",
		"--source-dir", "orders=" + dir,
		"--unverified-source", "orders",
		"--force",
		"--acknowledge-data-migrations",
		"--apply",
	})
	if err != nil {
		t.Fatalf("parsing a complete baseline command line failed: %v", err)
	}

	plan := opts.baseline.plan()
	if got := plan.Mapping["orders"]; !reflect.DeepEqual(got, []int64{17, 19, 20}) {
		t.Errorf("orders mapped to %v", got)
	}
	if !plan.Unverified["orders"] || !plan.Force || !plan.AcknowledgeDataMigrations {
		t.Errorf("the acceptances did not reach the plan: %+v", plan)
	}
	if !opts.baseline.apply {
		t.Error("--apply did not reach the options")
	}

	// The consumer source has to be registered, or its versions can be neither
	// checked against files nor written to a history table of its own.
	sources := opts.baseline.sources()
	if len(sources) != 1 || sources[0].Name != "orders" || sources[0].FS == nil {
		t.Fatalf("the consumer source was not built from --source-dir: %+v", sources)
	}
}

// The command a dry run prints is the one that performs it, so the two forms
// have to agree about what the plan is. They are written separately, which is
// exactly why this is pinned.
func TestVersionSpecRoundTrip(t *testing.T) {
	// The printed form is canonical — a contiguous run is always a range — so
	// what has to survive the round trip is the set of versions, not the text.
	for _, spec := range []string{"1-16", "1,2,3", "1-3,7,9-10", "5", "17,19-20", "1-2,4,6-8,11", "3,1-2"} {
		versions, err := parseVersionSpec(spec)
		if err != nil {
			t.Fatalf("parsing %q failed: %v", spec, err)
		}

		printed := formatVersionSpec(versions)
		again, err := parseVersionSpec(printed)
		if err != nil {
			t.Fatalf("the printed form of %q (%q) does not parse: %v", spec, printed, err)
		}
		if !reflect.DeepEqual(again, sortedVersions(versions)) {
			t.Errorf("%q printed as %q, which reads back as %v rather than %v", spec, printed, again, versions)
		}
	}

	for spec, want := range map[string]string{
		"1,2,3":    "1-3",
		"1-16":     "1-16",
		"17,19,20": "17,19-20",
		"5":        "5",
	} {
		versions, err := parseVersionSpec(spec)
		if err != nil {
			t.Fatalf("parsing %q failed: %v", spec, err)
		}
		if got := formatVersionSpec(versions); got != want {
			t.Errorf("%q printed as %q, want %q", spec, got, want)
		}
	}
}

func sortedVersions(versions []int64) []int64 {
	sorted := append([]int64(nil), versions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
}

func TestApplyCommandRestatesThePlan(t *testing.T) {
	dir := t.TempDir()

	opts, err := parseArgs([]string{
		"baseline",
		"--map", "orders:17,19-20",
		"--map", "core:1-16",
		"--source-dir", "orders=" + dir,
		"--unverified-source", "orders",
		"--force",
	})
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	// Core first, then the consumer: the order the runner applies them in, and
	// the order the plan above is reported in.
	want := "migrate baseline --map core:1-16 --map orders:17,19-20 " +
		"--source-dir orders=" + dir + " --unverified-source orders --force --apply"
	if got := applyCommand(opts.baseline); got != want {
		t.Fatalf("the apply command is\n  %s\nwant\n  %s", got, want)
	}
}

func versionsUpTo(n int64) []int64 {
	versions := make([]int64, 0, n)
	for v := int64(1); v <= n; v++ {
		versions = append(versions, v)
	}
	return versions
}
