package editor

import (
	"fmt"
	"os"
	"strings"

	"github.com/MichalHerstus/yaga/internal/fixer"
	"github.com/MichalHerstus/yaga/internal/parser"
	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// repairConfig runs the shared auto-repair engine over the current in-memory
// config. Whenever at least one fix applies, the pre-fix yaml is backed up to
// configPath+".bak" and the repaired yaml is written to configPath, then the
// editor's config is replaced with the repaired one (unsaved edits are part of
// the marshal, so they survive). Unfixable validation errors are returned
// alongside the applied fixes — partial repair still persists.
func (e *Editor) repairConfig() (fixed []string, remaining []error, err error) {
	data, err := yaml.Marshal(e.cfg)
	if err != nil {
		return nil, nil, err
	}
	out, fixed, remaining, err := fixer.Apply(data)
	if err != nil {
		return nil, nil, err
	}
	if len(fixed) == 0 {
		return nil, remaining, nil
	}
	var newCfg types.Config
	if err := yaml.Unmarshal(out, &newCfg); err != nil {
		return fixed, nil, err
	}
	if err := parser.Validate(&newCfg); err != nil {
		return fixed, nil, err
	}
	if err := os.WriteFile(e.configPath+".bak", data, 0644); err != nil {
		return fixed, nil, err
	}
	if err := os.WriteFile(e.configPath, out, 0644); err != nil {
		return fixed, nil, err
	}
	e.cfg = &newCfg
	e.modified = false
	e.saved = true
	e.refreshTitle()
	return fixed, remaining, nil
}

// autoFix is the Validate screen's "Fix" action: applies the shared fixer
// engine, writes the repair, and re-renders the findings. Mirrors `yaga
// validate --fix`.
func (e *Editor) autoFix() {
	fixed, remaining, err := e.repairConfig()
	if err != nil {
		e.errorModal("Fix failed", err.Error())
		return
	}
	if len(fixed) == 0 {
		if len(remaining) == 0 {
			e.toast("Nothing to fix")
		} else {
			e.errorModal("Nothing to fix", "Validation problems that cannot be auto-repaired:\n\n"+joinErrs(remaining))
		}
		return
	}
	e.refreshPage("Validate", e.validatePage())
	e.toast(fmt.Sprintf("Auto-fixed %d problem(s)", len(fixed)))
	if len(remaining) > 0 {
		e.errorModal("Fix left problems", "The following errors could not be auto-repaired:\n\n"+joinErrs(remaining))
	}
}

// joinErrs joins error strings, one per line.
func joinErrs(errs []error) string {
	lines := make([]string, 0, len(errs))
	for _, e := range errs {
		lines = append(lines, e.Error())
	}
	return strings.Join(lines, "\n")
}
