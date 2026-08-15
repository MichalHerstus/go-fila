// fix.go
//
// `yaga validate --fix` — CLI front end for the shared auto-repair engine in
// internal/fixer. `--dry-run` runs the same passes without writing anything.
package main

import (
	"fmt"
	"os"

	"github.com/MichalHerstus/yaga/internal/fixer"
)

// wantFixFlags scans os.Args[2:] for --fix and --dry-run. Both are unknown to
// parseGlobalFlags, so --config/--out etc. still resolve normally. -f is not
// aliased: it already means --force.
func wantFixFlags() (fix, dryRun bool) {
	for _, a := range os.Args[2:] {
		switch a {
		case "--fix":
			fix = true
		case "--dry-run":
			dryRun = true
		}
	}
	return fix, dryRun
}

// autoFixFile repairs known-fixable problems in the YAML config at path via
// fixer.Apply (surgical yaml.v3 node edits — config defaults are never
// injected). Whenever at least one fix applies, the original bytes are saved
// to path+".bak" first and the repaired file is written — even if unfixable
// validation errors remain, which are returned so the caller can report them
// (partial repair). dryRun mode never writes anything (no backup either).
// Unparseable YAML and write/backup failures are fatal errors.
func autoFixFile(path string, dryRun bool) (fixed []string, changed bool, remaining []error, err error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return nil, false, nil, fmt.Errorf("reading config file: %w", err)
	}
	out, fixed, remaining, err := fixer.Apply(orig)
	if err != nil {
		return fixed, false, nil, err
	}
	if len(fixed) == 0 {
		return nil, false, remaining, nil
	}
	if !dryRun {
		if err := os.WriteFile(path+".bak", orig, 0644); err != nil {
			return fixed, true, nil, fmt.Errorf("writing backup %s.bak: %w", path, err)
		}
		if err := os.WriteFile(path, out, 0644); err != nil {
			return fixed, true, nil, fmt.Errorf("writing config file: %w", err)
		}
	}
	return fixed, true, remaining, nil
}
