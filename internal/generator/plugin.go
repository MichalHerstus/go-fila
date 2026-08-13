// plugin.go
//
// Implements the generator-time plugin loader. Each declared plugin is built
// and run inside a throwaway Go module (a "shim") that imports the plugin's
// source, drives Register/Boot against a pkg/plugin.Panel builder and prints
// the resulting JSON manifest to stdout. The loader decodes that manifest and
// merges the plugin's contributions (resources, pages, navigation groups,
// hook attachments, SQL files) into the config before code generation.
// Plugin load failure is fatal: an explicitly declared plugin that fails to
// load is a config error.
package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MichalHerstus/yaga/internal/types"
	pluginapi "github.com/MichalHerstus/yaga/pkg/plugin"
)

// yagaModule is the module path of YAGA itself, used to locate a local
// checkout to `replace` into the shim (so the plugin compiles against the
// exact local sources without publishing to a proxy).
const yagaModule = "github.com/MichalHerstus/yaga"

// loadPlugins runs every declared plugin in config order and merges its
// manifest into the config. It is a no-op when plugins are skipped or none are
// declared. Plugin failures are fatal.
// Returns: an error naming the failing plugin.
func (g *Generator) loadPlugins() error {
	if g.SkipPlugins || len(g.Config.Plugins) == 0 {
		return nil
	}
	for _, p := range g.Config.Plugins {
		if err := g.loadPlugin(p); err != nil {
			return fmt.Errorf("plugin %q: %w", p.Name, err)
		}
	}
	return nil
}

// loadPlugin loads a single plugin: resolves its source, writes and runs the
// shim, decodes the manifest and merges it.
// Params: p (the plugin declaration from the config).
// Returns: an error on any load or merge failure.
func (g *Generator) loadPlugin(p types.PluginConfig) error {
	modPath, localDir, err := resolvePluginSource(p.Source)
	if err != nil {
		return err
	}

	// Clone the config and inject the detected database driver so plugins can
	// emit driver-appropriate SQL (sqlc placeholders and DDL differ per
	// driver). The reserved "driver" key overrides any user-provided value.
	config := map[string]any{}
	for k, v := range p.Config {
		config[k] = v
	}
	config["driver"] = g.driver()

	shimDir, err := os.MkdirTemp("", "yaga-plugin-shim")
	if err != nil {
		return fmt.Errorf("creating shim dir: %w", err)
	}
	defer os.RemoveAll(shimDir)

	yagaCheckout := findYagaCheckout(localDir)
	if err := writeShim(shimDir, modPath, localDir, yagaCheckout, config); err != nil {
		return err
	}

	if err := runShim(shimDir); err != nil {
		return err
	}

	raw, err := os.ReadFile(filepath.Join(shimDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	var m pluginapi.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("decoding manifest: %w\n%s", err, raw)
	}

	if err := g.mergeManifest(p, m); err != nil {
		return err
	}
	if g.Verbose {
		fmt.Printf("plugin %q loaded: %d resource(s), %d page(s), %d nav group(s), %d SQL file(s), %d hook attachment(s), %d hook source(s)\n",
			p.Name, len(m.Resources), len(m.Pages), len(m.Navigation), len(m.SQLFiles), len(m.HookAttachments), len(m.HookSources))
	}
	return nil
}

// resolvePluginSource classifies a plugin source string. Local directories
// (".", "/", "~" prefixes) are resolved to an absolute path and their module
// path is read from their go.mod; anything else is treated as a Go module
// import path.
// Returns: the module path, the absolute local directory (empty for module
// paths), or an error.
func resolvePluginSource(source string) (modPath, localDir string, err error) {
	if source == "" {
		return "", "", fmt.Errorf("plugin source is empty")
	}
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") {
		abs, err := filepath.Abs(source)
		if err != nil {
			return "", "", fmt.Errorf("resolving source %q: %w", source, err)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return "", "", fmt.Errorf("source %q is not a directory", source)
		}
		mod := modulePathFromGoMod(filepath.Join(abs, "go.mod"))
		if mod == "" {
			return "", "", fmt.Errorf("source %q has no go.mod with a module directive", source)
		}
		return mod, abs, nil
	}
	return source, "", nil
}

// modulePathFromGoMod extracts the `module` directive from a go.mod file.
// Returns: the module path, or "" when the file is missing or malformed.
func modulePathFromGoMod(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// findYagaCheckout walks up from the given starting directories looking for
// a go.mod declaring module github.com/MichalHerstus/yaga, so the shim can
// `replace` it with the local checkout (plugins then compile against the exact
// local sources). Returns the checkout directory or "" when not found.
// Params: startDirs (additional starting points, e.g. the plugin source dir).
func findYagaCheckout(startDirs ...string) string {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	starts = append(starts, startDirs...)
	for _, start := range starts {
		if start == "" {
			continue
		}
		for dir := start; dir != "" && dir != "/"; dir = filepath.Dir(dir) {
			if modulePathFromGoMod(filepath.Join(dir, "go.mod")) == yagaModule {
				return dir
			}
		}
	}
	return ""
}

// writeShim writes the throwaway module (go.mod + main.go) that runs the
// plugin. main.go builds a panel, calls the plugin's Configure/Register/Boot
// and writes the JSON manifest to manifest.json. The plugin package is
// imported under the fixed alias "plug" so the actual package name does not
// matter; the plugin's `New` must be exported.
// Params: dir (shim directory), modPath (plugin module path), localDir
// (absolute plugin dir when local, else ""), yagaCheckout (local YAGA
// checkout dir when found, else ""), config (the YAML config map, including
// the injected "driver" key).
// Returns: an error on write failure.
func writeShim(dir, modPath, localDir, yagaCheckout string, config map[string]any) error {
	var b strings.Builder
	b.WriteString("module yaga-plugin-shim\n\ngo 1.26.3\n\nrequire (\n")
	b.WriteString("\t" + yagaModule + " v0.0.0\n")
	b.WriteString("\t" + modPath + " v0.0.0\n")
	b.WriteString(")\n")
	if yagaCheckout != "" {
		b.WriteString("\nreplace " + yagaModule + " => " + yagaCheckout + "\n")
	}
	if localDir != "" {
		b.WriteString("replace " + modPath + " => " + localDir + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing shim go.mod: %w", err)
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshalling plugin config: %w", err)
	}

	main := fmt.Sprintf(`package main

import (
    "encoding/json"
    "fmt"
    "os"

    pluginapi %q
    plug %q
)

func main() {
    p := pluginapi.NewPanel()
    instance := plug.New()

    var configMap map[string]interface{}
    if err := json.Unmarshal([]byte(%q), &configMap); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if c, ok := instance.(pluginapi.Configurer); ok {
        if err := c.Configure(configMap); err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }
    }
    if err := instance.Register(p); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if err := instance.Boot(p); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    out, err := json.MarshalIndent(p.Manifest(), "", "  ")
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if err := os.WriteFile("manifest.json", out, 0644); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
`, "github.com/MichalHerstus/yaga/pkg/plugin", modPath, string(configJSON))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0644); err != nil {
		return fmt.Errorf("writing shim main.go: %w", err)
	}
	return nil
}

// runShim runs `go mod tidy` then `go run .` in the shim directory, which
// produces manifest.json. Tidy resolves the plugin module (from the proxy for
// module-path sources, or via replace for local dirs).
// Params: dir (shim directory).
// Returns: an error including the tool output on failure.
func runShim(dir string) error {
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidyOut, err := tidy.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy failed: %w\n%s", err, tidyOut)
	}
	run := exec.Command("go", "run", ".")
	run.Dir = dir
	runOut, runErr := run.CombinedOutput()
	if err != nil {
		return fmt.Errorf("plugin execution failed: %w\n%s", err, runErr)
	}
	if len(runOut) > 0 {
		return fmt.Errorf("plugin wrote unexpected output to stdout: %s", runOut)
	}
	return nil
}

// mergeManifest merges a decoded plugin manifest into the config: resources,
// pages and navigation groups are appended; hook sources are written into the
// output dir's internal/hooks/ and their function names tracked so generateHooks
// skips stubs for them; hook attachments are resolved against the (merged)
// resource set; SQL files are written into the output dir, never overwriting
// existing files. fn hooks from plugins are allowed when the fn name is backed
// by one of the plugin's hook sources.
// Params: p (the plugin declaration, for error messages), m (the manifest).
// Returns: an error describing the first merge problem.
func (g *Generator) mergeManifest(p types.PluginConfig, m pluginapi.Manifest) error {
	if g.pluginFnNames == nil {
		g.pluginFnNames = map[string]bool{}
		g.pluginHookFiles = map[string]string{}
	}

	for name, content := range m.HookSources {
		if err := pluginapi.ValidateHookSourceName(name); err != nil {
			return fmt.Errorf("plugin %q: %w", p.Name, err)
		}
		g.pluginHookFiles[name] = content
		for _, fn := range hookFuncNames(content) {
			g.pluginFnNames[fn] = true
		}
		dst := filepath.Join(g.OutDir, "internal/hooks", name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("plugin %q: creating hooks dir: %w", p.Name, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			return fmt.Errorf("plugin %q: writing %s: %w", p.Name, name, err)
		}
	}

	for _, r := range m.Resources {
		if err := g.resourceNameTaken(r.Name); err != nil {
			return err
		}
		g.Config.Resources = append(g.Config.Resources, r)
	}
	for _, pg := range m.Pages {
		if err := g.pageNameTaken(pg.Name); err != nil {
			return err
		}
		g.Config.Pages = append(g.Config.Pages, pg)
	}
	g.Config.Navigation = append(g.Config.Navigation, m.Navigation...)

	for _, att := range m.HookAttachments {
		if err := g.attachHook(p.Name, att); err != nil {
			return err
		}
	}

	for name, content := range m.SQLFiles {
		if err := pluginapi.ValidateSQLFileName(name); err != nil {
			return fmt.Errorf("plugin %q: %w", p.Name, err)
		}
		dst := filepath.Join(g.OutDir, "sql", name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("plugin %q: creating sql dir: %w", p.Name, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			return fmt.Errorf("plugin %q: writing %s: %w", p.Name, name, err)
		}
	}
	return nil
}

// hookFuncNames scans a plugin hook source for package-level function
// declarations and returns their names. These identify fn hooks whose
// implementation the plugin provides, so generateHooks must not emit a stub
// for them (a stub would be a duplicate definition in the same package).
// Methods (func with a receiver) and generic functions are skipped.
// Params: src (the hook source file content).
// Returns: the function names in declaration order.
func hookFuncNames(src string) []string {
	var fns []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "func ")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, "(")
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "(") || strings.ContainsAny(name, "[]*{}") {
			continue
		}
		fns = append(fns, name)
	}
	return fns
}

// resourceNameTaken returns an error when a resource with the given name
// already exists in the config (from YAML or an earlier plugin).
func (g *Generator) resourceNameTaken(name string) error {
	for _, r := range g.Config.Resources {
		if r.Name == name {
			return fmt.Errorf("resource %q already exists", name)
		}
	}
	return nil
}

// pageNameTaken returns an error when a page with the given name already
// exists in the config.
func (g *Generator) pageNameTaken(name string) error {
	for _, pg := range g.Config.Pages {
		if pg.Name == name {
			return fmt.Errorf("page %q already exists", name)
		}
	}
	return nil
}

// attachHook resolves one hook attachment against the merged resource set and
// appends the hook to the target action's before/after list. fn hooks are
// allowed when the fn name is backed by a plugin hook source (AddHookSource);
// an fn hook without a matching source is fatal. The hook's SQL binds the
// current record id as $1; proc hooks bind it the same way (postgres/mssql,
// ignored on sqlite).
// Params: pluginName (for error messages), att (the attachment).
// Returns: an error for a missing resource/action or an unbacked fn hook.
func (g *Generator) attachHook(pluginName string, att pluginapi.HookAttachment) error {
	if att.Hook.Fn != "" && !g.pluginFnNames[att.Hook.Fn] {
		return fmt.Errorf("plugin %q: fn hook %q has no matching hook source — add AddHookSource with the implementation, or use sql/proc", pluginName, att.Hook.Fn)
	}
	if att.Hook.SQL == "" && att.Hook.Proc == "" && att.Hook.Fn == "" {
		return fmt.Errorf("plugin %q: hook on %s.%s.%s must set sql, proc or fn", pluginName, att.Resource, att.Action, att.When)
	}
	if att.Hook.Name == "" {
		att.Hook.Name = pluginName + "_" + att.Action + "_" + att.When
	}
	var target *types.Resource
	for i := range g.Config.Resources {
		if g.Config.Resources[i].Name == att.Resource {
			target = &g.Config.Resources[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("plugin %q: hook target resource %q not found", pluginName, att.Resource)
	}
	hooks := hookTargetHooks(target, att.Action)
	if hooks == nil {
		return fmt.Errorf("plugin %q: hook target action %q not found on resource %q", pluginName, att.Action, att.Resource)
	}
	if att.When == "before" {
		hooks.Before = append(hooks.Before, att.Hook)
	} else {
		hooks.After = append(hooks.After, att.Hook)
	}
	return nil
}

// hookTargetHooks returns the Hooks block of the named action on a resource,
// creating it when missing. For form actions the resource must declare the
// matching FormAction; custom actions are matched by name. Returns nil when
// the action does not exist.
// Params: r (the resource), action (create/update/delete or action name).
func hookTargetHooks(r *types.Resource, action string) *types.Hooks {
	if r.Form != nil {
		switch action {
		case "create":
			if r.Form.Create != nil {
				if r.Form.Create.Hooks == nil {
					r.Form.Create.Hooks = &types.Hooks{}
				}
				return r.Form.Create.Hooks
			}
			return nil
		case "update":
			if r.Form.Update != nil {
				if r.Form.Update.Hooks == nil {
					r.Form.Update.Hooks = &types.Hooks{}
				}
				return r.Form.Update.Hooks
			}
			return nil
		case "delete":
			if r.Form.Delete != nil {
				if r.Form.Delete.Hooks == nil {
					r.Form.Delete.Hooks = &types.Hooks{}
				}
				return r.Form.Delete.Hooks
			}
			return nil
		}
	}
	for i := range r.Actions {
		if r.Actions[i].Name == action {
			if r.Actions[i].Hooks == nil {
				r.Actions[i].Hooks = &types.Hooks{}
			}
			return r.Actions[i].Hooks
		}
	}
	return nil
}
