package yc1

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultRepoURL = "https://github.com/yingca1/yc1.git"
	githubAPIURL   = "https://api.github.com/repos/yingca1/yc1/releases/latest"
	linkMode       = "link"
	copyMode       = "copy"
)

var (
	minimalProfileConfigs = []string{"git", "curl", "wget"}
	defaultProfileConfigs = []string{"curl", "git", "kitty", "tmux", "vim", "wget", "zsh"}
	profileVarPattern     = regexp.MustCompile(`\$\{vars\.([A-Za-z0-9_.-]+)\}`)
)

type App struct {
	Version string
	Out     io.Writer
	Err     io.Writer
	Root    string
	RepoURL string
}

type Profile struct {
	Version int               `json:"version" yaml:"version"`
	Vars    map[string]string `json:"vars" yaml:"vars"`
	Configs []string          `json:"configs" yaml:"configs"`
	Skills  []SkillRef        `json:"skills" yaml:"skills"`
}

type SkillRef struct {
	Name    string   `json:"name" yaml:"name"`
	Source  string   `json:"source" yaml:"source"`
	Targets []string `json:"targets" yaml:"targets"`
}

type profileSelection struct {
	Profile Profile
	BaseDir string
	Name    string
}

type ConfigManifest struct {
	Name         string        `json:"name" yaml:"name"`
	Description  string        `json:"description" yaml:"description"`
	Files        []FileMapping `json:"files" yaml:"files"`
	LocalFiles   []string      `json:"local_files" yaml:"local_files"`
	Dependencies []Dependency  `json:"dependencies" yaml:"dependencies"`
}

type FileMapping struct {
	Source string   `json:"source" yaml:"source"`
	Target string   `json:"target" yaml:"target"`
	OS     []string `json:"os" yaml:"os"`
}

type Dependency struct {
	Name    string            `json:"name" yaml:"name"`
	Check   DependencyCheck   `json:"check" yaml:"check"`
	Install map[string]string `json:"install" yaml:"install"`
}

type DependencyCheck struct {
	Command  string   `json:"command" yaml:"command"`
	Commands []string `json:"commands" yaml:"commands"`
	Path     string   `json:"path" yaml:"path"`
	Paths    []string `json:"paths" yaml:"paths"`
	Probe    string   `json:"probe" yaml:"probe"`
	Probes   []string `json:"probes" yaml:"probes"`
}

type dependencyCheckCandidate struct {
	Kind  string
	Value string
}

type dependencyCheckStatus struct {
	State string
	Lines []string
}

type ConfigState struct {
	Config    string      `json:"config"`
	Mode      string      `json:"mode"`
	Files     []FileState `json:"files"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type FileState struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Checksum string `json:"checksum,omitempty"`
	Backup   string `json:"backup,omitempty"`
}

type SkillState struct {
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	TargetKey string    `json:"target_key"`
	TargetDir string    `json:"target_dir"`
	LinkPath  string    `json:"link_path"`
	UpdatedAt time.Time `json:"updated_at"`
}

type actionOptions struct {
	Mode        string
	File        string
	ProfileName string
	Names       []string
}

func NewApp(version string, out, err io.Writer) *App {
	root := os.Getenv("YC1_ROOT")
	if root == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			root = filepath.Join(home, ".config", "yc1")
		}
	}
	repoURL := os.Getenv("YC1_REPO_URL")
	if repoURL == "" {
		repoURL = defaultRepoURL
	}
	return &App{
		Version: version,
		Out:     out,
		Err:     err,
		Root:    root,
		RepoURL: repoURL,
	}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printHelp()
		return 0
	}

	var err error
	switch args[0] {
	case "up":
		err = a.runProfileAction("up", args[1:])
	case "down":
		err = a.runProfileAction("down", args[1:])
	case "status":
		err = a.runProfileAction("status", args[1:])
	case "config", "configs":
		err = a.runConfigCommand(args[1:])
	case "skill", "skills":
		err = a.runSkillCommand(args[1:])
	case "pull":
		err = a.runPull()
	case "update":
		err = a.runUpdate()
	case "version":
		fmt.Fprintf(a.Out, "yc1 %s\n", a.Version)
	case "help", "-h", "--help":
		a.printHelp()
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}

	if err != nil {
		fmt.Fprintf(a.Err, "yc1: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) printHelp() {
	fmt.Fprintln(a.Out, `yc1 manages configs and agent skills.

Usage:
  yc1 up|down|status [-f yc1.yml|-p profile] [--link|--copy]
  yc1 config up|down|status [name...] [--link|--copy]
  yc1 skill up|down|status [name...]  # reads ./yc1.yml
  yc1 pull
  yc1 update
  yc1 version

Profiles:
  -f, --file       Read an explicit yc1.yml profile
  -p, --profile    Use a named profile from _profiles/, profiles/, or built-ins

Defaults:
  root: ~/.config/yc1
  source: ~/.config/yc1/source
  config mode: symlink

Bare up/down/status reads ./yc1.yml. If it does not exist, pass -f or -p.`)
}

func (a *App) runProfileAction(action string, args []string) error {
	opts, err := parseTopLevelActionArgs(args)
	if err != nil {
		return err
	}
	if action == "up" && opts.ProfileName != "" {
		if err := a.ensureSource(); err != nil {
			return err
		}
	}
	selection, err := a.selectProfile(opts)
	if err != nil {
		return err
	}
	if len(selection.Profile.Configs) == 0 && len(selection.Profile.Skills) == 0 {
		return fmt.Errorf("profile %s has no configs or skills", selection.Name)
	}

	switch action {
	case "up":
		if len(selection.Profile.Configs) > 0 {
			if err := a.ensureSource(); err != nil {
				return err
			}
		}
		if len(selection.Profile.Configs) > 0 {
			if err := a.upConfigs(selection.Profile.Configs, opts.Mode); err != nil {
				return err
			}
		}
		for _, skill := range selection.Profile.Skills {
			if err := a.upSkill(skill, selection.BaseDir); err != nil {
				return err
			}
		}
	case "down":
		if len(selection.Profile.Configs) > 0 {
			if err := a.downConfigs(selection.Profile.Configs); err != nil {
				return err
			}
		}
		for _, skill := range selection.Profile.Skills {
			if err := a.downSkill(skill, selection.BaseDir); err != nil {
				return err
			}
		}
	case "status":
		if len(selection.Profile.Configs) > 0 {
			fmt.Fprintln(a.Out, "Configs")
			for _, name := range selection.Profile.Configs {
				for _, line := range a.configStatusLines(name) {
					fmt.Fprintln(a.Out, "  "+line)
				}
			}
		}
		if len(selection.Profile.Skills) > 0 {
			fmt.Fprintln(a.Out, "Skills")
			for _, skill := range selection.Profile.Skills {
				for _, line := range a.skillStatus(skill, selection.BaseDir) {
					fmt.Fprintln(a.Out, "  "+line)
				}
			}
		}
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	return nil
}

func (a *App) runConfigCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: yc1 config up|down|status [name...] [--link|--copy]")
	}
	action := args[0]
	opts, err := parseResourceArgs(args[1:], true)
	if err != nil {
		return err
	}
	names := opts.Names
	if len(names) == 0 {
		names, err = a.resolveConfigNamesFromStateOrSource()
		if err != nil {
			return err
		}
	}
	switch action {
	case "up":
		if err := a.ensureSource(); err != nil {
			return err
		}
		return a.upConfigs(names, opts.Mode)
	case "down":
		return a.downConfigs(names)
	case "status":
		for _, name := range names {
			for _, line := range a.configStatusLines(name) {
				fmt.Fprintln(a.Out, line)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown config command %q", action)
	}
}

func (a *App) runSkillCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: yc1 skill up|down|status [name...]")
	}
	action := args[0]
	opts, err := parseResourceArgs(args[1:], false)
	if err != nil {
		return err
	}
	if action != "up" && action != "down" && action != "status" {
		return fmt.Errorf("unknown skill command %q", action)
	}
	selection, err := a.selectProfile(actionOptions{})
	if err != nil {
		return err
	}
	skills, err := selectSkills(selection.Profile.Skills, opts.Names)
	if err != nil {
		return err
	}
	switch action {
	case "up":
		for _, skill := range skills {
			if err := a.upSkill(skill, selection.BaseDir); err != nil {
				return err
			}
		}
	case "down":
		for _, skill := range skills {
			if err := a.downSkill(skill, selection.BaseDir); err != nil {
				return err
			}
		}
	case "status":
		for _, skill := range skills {
			for _, line := range a.skillStatus(skill, selection.BaseDir) {
				fmt.Fprintln(a.Out, line)
			}
		}
	default:
		return fmt.Errorf("unknown skill command %q", action)
	}
	return nil
}

func parseTopLevelActionArgs(args []string) (actionOptions, error) {
	opts := actionOptions{Mode: linkMode}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--link":
			opts.Mode = linkMode
		case arg == "--copy":
			opts.Mode = copyMode
		case arg == "-f" || arg == "--file":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a path", arg)
			}
			opts.File = args[i]
		case strings.HasPrefix(arg, "--file="):
			opts.File = strings.TrimPrefix(arg, "--file=")
		case arg == "-p" || arg == "--profile":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a profile name", arg)
			}
			opts.ProfileName = args[i]
		case strings.HasPrefix(arg, "--profile="):
			opts.ProfileName = strings.TrimPrefix(arg, "--profile=")
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			return opts, fmt.Errorf("top-level %q uses profiles; use yc1 config or yc1 skill for resource names", arg)
		}
	}
	if opts.File != "" && opts.ProfileName != "" {
		return opts, errors.New("-f and -p are mutually exclusive")
	}
	return opts, nil
}

func parseResourceArgs(args []string, allowMode bool) (actionOptions, error) {
	opts := actionOptions{Mode: linkMode}
	for _, arg := range args {
		switch arg {
		case "--link":
			if !allowMode {
				return opts, fmt.Errorf("unknown flag %q", arg)
			}
			opts.Mode = linkMode
		case "--copy":
			if !allowMode {
				return opts, fmt.Errorf("unknown flag %q", arg)
			}
			opts.Mode = copyMode
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown flag %q", arg)
			}
			opts.Names = append(opts.Names, arg)
		}
	}
	return opts, nil
}

func selectSkills(skills []SkillRef, names []string) ([]SkillRef, error) {
	if len(skills) == 0 {
		return nil, errors.New("profile has no skills")
	}
	if len(names) == 0 {
		return skills, nil
	}
	requested := map[string]bool{}
	for _, name := range names {
		requested[name] = false
	}
	var selected []SkillRef
	for _, skill := range skills {
		if _, ok := requested[skill.Name]; ok {
			selected = append(selected, skill)
			requested[skill.Name] = true
		}
	}
	var missing []string
	for name, found := range requested {
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("profile does not define skill(s): %s", strings.Join(missing, ", "))
	}
	return selected, nil
}

func (a *App) selectProfile(opts actionOptions) (profileSelection, error) {
	if opts.ProfileName != "" {
		return a.selectNamedProfile(opts.ProfileName)
	}
	path := opts.File
	if path == "" {
		path = "yc1.yml"
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return profileSelection{}, errors.New("yc1.yml not found in current directory; pass -f or -p")
			}
			return profileSelection{}, err
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return profileSelection{}, err
	}
	profile, err := readProfileFile(abs)
	if err != nil {
		return profileSelection{}, err
	}
	return profileSelection{Profile: profile, BaseDir: filepath.Dir(abs), Name: abs}, nil
}

func (a *App) selectNamedProfile(name string) (profileSelection, error) {
	if err := validateProfileName(name); err != nil {
		return profileSelection{}, err
	}
	for _, dir := range []string{a.localProfilesDir(), a.profilesDir()} {
		path := filepath.Join(dir, name, "yc1.yml")
		if _, err := os.Stat(path); err == nil {
			profile, err := readProfileFile(path)
			if err != nil {
				return profileSelection{}, err
			}
			return profileSelection{Profile: profile, BaseDir: filepath.Dir(path), Name: name}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return profileSelection{}, err
		}
	}
	profile, err := builtinProfile(name)
	if err != nil {
		return profileSelection{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return profileSelection{}, err
	}
	return profileSelection{Profile: profile, BaseDir: cwd, Name: name}, nil
}

func validateProfileName(name string) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	return nil
}

func builtinProfile(name string) (Profile, error) {
	switch name {
	case "minimal":
		return Profile{Version: 1, Configs: append([]string(nil), minimalProfileConfigs...)}, nil
	case "default":
		return Profile{Version: 1, Configs: append([]string(nil), defaultProfileConfigs...)}, nil
	default:
		return Profile{}, fmt.Errorf("unknown profile %q", name)
	}
}

func readProfileFile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return Profile{}, err
	}
	if err := expandProfileVars(&profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func expandProfileVars(profile *Profile) error {
	if len(profile.Vars) == 0 {
		return expandProfileSkillVars(profile, nil)
	}
	resolved := map[string]string{}
	var resolve func(string, map[string]bool) (string, error)
	resolve = func(name string, stack map[string]bool) (string, error) {
		if value, ok := resolved[name]; ok {
			return value, nil
		}
		raw, ok := profile.Vars[name]
		if !ok {
			return "", fmt.Errorf("unknown profile variable %q", name)
		}
		if stack[name] {
			return "", fmt.Errorf("profile variable %q has a circular reference", name)
		}
		stack[name] = true
		value, err := expandProfileString(raw, resolve, stack)
		delete(stack, name)
		if err != nil {
			return "", err
		}
		resolved[name] = value
		return value, nil
	}
	for name := range profile.Vars {
		value, err := resolve(name, map[string]bool{})
		if err != nil {
			return err
		}
		profile.Vars[name] = value
	}
	return expandProfileSkillVars(profile, resolved)
}

func expandProfileSkillVars(profile *Profile, vars map[string]string) error {
	resolve := func(name string, _ map[string]bool) (string, error) {
		value, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("unknown profile variable %q", name)
		}
		return value, nil
	}
	for skillIndex := range profile.Skills {
		source, err := expandProfileString(profile.Skills[skillIndex].Source, resolve, nil)
		if err != nil {
			return err
		}
		profile.Skills[skillIndex].Source = source
		for targetIndex := range profile.Skills[skillIndex].Targets {
			path, err := expandProfileString(profile.Skills[skillIndex].Targets[targetIndex], resolve, nil)
			if err != nil {
				return err
			}
			profile.Skills[skillIndex].Targets[targetIndex] = path
		}
	}
	return nil
}

func expandProfileString(input string, resolve func(string, map[string]bool) (string, error), stack map[string]bool) (string, error) {
	matches := profileVarPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, nil
	}
	var builder strings.Builder
	last := 0
	for _, match := range matches {
		builder.WriteString(input[last:match[0]])
		name := input[match[2]:match[3]]
		value, err := resolve(name, stack)
		if err != nil {
			return "", err
		}
		builder.WriteString(value)
		last = match[1]
	}
	builder.WriteString(input[last:])
	return builder.String(), nil
}

func (a *App) upConfigs(names []string, mode string) error {
	for _, name := range names {
		manifest, err := a.loadConfigManifest(name)
		if err != nil {
			return err
		}
		if err := a.ensureLocalFiles(name, manifest); err != nil {
			return err
		}
		if err := a.installDependencies(manifest); err != nil {
			return fmt.Errorf("config %s: %w", name, err)
		}
		if err := a.activateConfig(name, manifest, mode); err != nil {
			return fmt.Errorf("config %s: %w", name, err)
		}
		fmt.Fprintf(a.Out, "config/%s: up (%s)\n", name, mode)
	}
	return nil
}

func (a *App) downConfigs(names []string) error {
	for _, name := range names {
		state, err := a.readConfigState(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(a.Out, "config/%s: down\n", name)
				continue
			}
			return err
		}
		if err := a.deactivateConfig(state); err != nil {
			return fmt.Errorf("config %s: %w", name, err)
		}
		if err := os.Remove(a.configStatePath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintf(a.Out, "config/%s: down\n", name)
	}
	return nil
}

func (a *App) resolveConfigNamesFromStateOrSource() ([]string, error) {
	names := map[string]bool{}
	if sourceNames, err := a.allConfigNamesFromSource(); err == nil {
		for _, name := range sourceNames {
			names[name] = true
		}
	}
	stateEntries, err := os.ReadDir(a.configStateDir())
	if err == nil {
		for _, entry := range stateEntries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				names[strings.TrimSuffix(entry.Name(), ".json")] = true
			}
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no configs found")
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted, nil
}

func (a *App) allConfigNamesFromSource() ([]string, error) {
	entries, err := os.ReadDir(a.configsDir())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(a.configsDir(), entry.Name(), "yc1.yml")); err == nil {
				names = append(names, entry.Name())
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

func (a *App) loadConfigManifest(name string) (ConfigManifest, error) {
	path := filepath.Join(a.configsDir(), name, "yc1.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigManifest{}, err
	}
	var manifest ConfigManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return ConfigManifest{}, err
	}
	if manifest.Name == "" {
		manifest.Name = name
	}
	if manifest.Name != name {
		return ConfigManifest{}, fmt.Errorf("manifest name %q does not match config %q", manifest.Name, name)
	}
	return manifest, nil
}

func (a *App) ensureLocalFiles(name string, manifest ConfigManifest) error {
	for _, local := range manifest.LocalFiles {
		path := filepath.Join(a.Root, "local", name, local)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) installDependencies(manifest ConfigManifest) error {
	for _, dep := range manifest.Dependencies {
		ok, err := dependencySatisfied(dep)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		cmd := dep.Install[runtime.GOOS]
		if cmd == "" {
			return fmt.Errorf("dependency %s is missing and has no %s installer", dep.Name, runtime.GOOS)
		}
		fmt.Fprintf(a.Out, "%s: installing dependency %s\n", manifest.Name, dep.Name)
		if err := runShell(cmd); err != nil {
			return fmt.Errorf("install dependency %s: %w", dep.Name, err)
		}
	}
	return nil
}

func dependencySatisfied(dep Dependency) (bool, error) {
	candidates := dependencyCheckCandidates(dep.Check)
	if len(candidates) == 0 {
		return true, nil
	}
	for _, candidate := range candidates {
		ok, _, err := dependencyCheckCandidateSatisfied(candidate)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func dependencyCheckCandidates(check DependencyCheck) []dependencyCheckCandidate {
	var candidates []dependencyCheckCandidate
	if check.Command != "" {
		candidates = append(candidates, dependencyCheckCandidate{Kind: "command", Value: check.Command})
	}
	for _, command := range check.Commands {
		if command != "" {
			candidates = append(candidates, dependencyCheckCandidate{Kind: "command", Value: command})
		}
	}
	if check.Probe != "" {
		candidates = append(candidates, dependencyCheckCandidate{Kind: "probe", Value: check.Probe})
	}
	for _, probe := range check.Probes {
		if probe != "" {
			candidates = append(candidates, dependencyCheckCandidate{Kind: "probe", Value: probe})
		}
	}
	if check.Path != "" {
		candidates = append(candidates, dependencyCheckCandidate{Kind: "path", Value: check.Path})
	}
	for _, path := range check.Paths {
		if path != "" {
			candidates = append(candidates, dependencyCheckCandidate{Kind: "path", Value: path})
		}
	}
	return candidates
}

func dependencyCheckCandidateSatisfied(candidate dependencyCheckCandidate) (bool, string, error) {
	switch candidate.Kind {
	case "command":
		if _, err := exec.LookPath(candidate.Value); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return false, "not found", nil
			}
			return false, "", err
		}
		return true, "found", nil
	case "probe":
		ok, err := dependencyProbeSatisfied(candidate.Value)
		if err != nil {
			return false, "", err
		}
		if !ok {
			return false, "failed", nil
		}
		return true, "passed", nil
	case "path":
		path, err := expandPath(candidate.Value)
		if err != nil {
			return false, "", err
		}
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, "missing", nil
			}
			return false, "", err
		}
		return true, "exists", nil
	default:
		return false, "", fmt.Errorf("unknown dependency check kind %q", candidate.Kind)
	}
}

func dependencyCheckStatusLines(dep Dependency) dependencyCheckStatus {
	candidates := dependencyCheckCandidates(dep.Check)
	if len(candidates) == 0 {
		return dependencyCheckStatus{
			State: "ok",
			Lines: []string{fmt.Sprintf("check/%s: ok (no checks)", dep.Name)},
		}
	}
	var failures []string
	for _, candidate := range candidates {
		ok, detail, err := dependencyCheckCandidateSatisfied(candidate)
		label := fmt.Sprintf("%s: %s", candidate.Kind, candidate.Value)
		if err != nil {
			return dependencyCheckStatus{
				State: "error",
				Lines: []string{fmt.Sprintf("check/%s: error (%s: %v)", dep.Name, label, err)},
			}
		}
		if ok {
			return dependencyCheckStatus{
				State: "ok",
				Lines: []string{fmt.Sprintf("check/%s: ok (%s)", dep.Name, label)},
			}
		}
		failures = append(failures, fmt.Sprintf("  %s -> %s", label, detail))
	}
	lines := []string{fmt.Sprintf("check/%s: missing", dep.Name)}
	lines = append(lines, failures...)
	return dependencyCheckStatus{State: "missing", Lines: lines}
}

func dependencyProbeSatisfied(probe string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", probe)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return false, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *App) activateConfig(name string, manifest ConfigManifest, mode string) error {
	prev, _ := a.readConfigState(name)
	prevByTarget := map[string]FileState{}
	for _, file := range prev.Files {
		prevByTarget[file.Target] = file
	}

	next := ConfigState{Config: name, Mode: mode, UpdatedAt: time.Now().UTC()}
	for _, mapping := range manifest.Files {
		if !mapping.appliesTo(runtime.GOOS) {
			continue
		}
		source := filepath.Join(a.configsDir(), name, mapping.Source)
		target, err := expandPath(mapping.Target)
		if err != nil {
			return err
		}
		if _, err := os.Stat(source); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		state := FileState{Source: source, Target: target, Mode: mode}
		if prevState, ok := prevByTarget[target]; ok {
			state.Backup = prevState.Backup
		}
		backup, err := a.prepareTarget(target, source, prevByTarget[target])
		if err != nil {
			return err
		}
		if backup != "" && state.Backup == "" {
			state.Backup = backup
		}
		if mode == linkMode {
			if err := os.Symlink(source, target); err != nil {
				return err
			}
		} else {
			if err := copyFile(source, target); err != nil {
				return err
			}
			sum, err := checksumFile(target)
			if err != nil {
				return err
			}
			state.Checksum = sum
		}
		next.Files = append(next.Files, state)
	}
	if len(next.Files) == 0 {
		return fmt.Errorf("no files apply to %s on %s", name, runtime.GOOS)
	}
	return a.writeConfigState(next)
}

func (mapping FileMapping) appliesTo(goos string) bool {
	if len(mapping.OS) == 0 {
		return true
	}
	for _, allowed := range mapping.OS {
		if allowed == goos {
			return true
		}
	}
	return false
}

func (a *App) prepareTarget(target, source string, previous FileState) (string, error) {
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	if previous.Target != "" {
		switch previous.Mode {
		case linkMode:
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(target)
				if err == nil && (linkTarget == previous.Source || linkTarget == source) {
					return "", os.Remove(target)
				}
			}
			return a.backupTarget(target)
		case copyMode:
			if info.Mode().IsRegular() {
				sum, err := checksumFile(target)
				if err == nil && previous.Checksum != "" && sum == previous.Checksum {
					return "", os.Remove(target)
				}
			}
			return a.backupTarget(target)
		default:
			return a.backupTarget(target)
		}
	}

	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(target)
		if err == nil && linkTarget == source {
			return "", os.Remove(target)
		}
	}
	return a.backupTarget(target)
}

func (a *App) backupTarget(target string) (string, error) {
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	backup := filepath.Join(a.Root, "backups", time.Now().UTC().Format("20060102T150405.000000000Z"), sanitizePath(target))
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(target, backup); err != nil {
		return "", err
	}
	return backup, nil
}

func (a *App) deactivateConfig(state ConfigState) error {
	for _, file := range state.Files {
		if info, err := os.Lstat(file.Target); err == nil {
			switch file.Mode {
			case linkMode:
				if info.Mode()&os.ModeSymlink != 0 {
					linkTarget, err := os.Readlink(file.Target)
					if err == nil && linkTarget == file.Source {
						if err := os.Remove(file.Target); err != nil {
							return err
						}
						break
					}
				}
				if _, err := a.backupTarget(file.Target); err != nil {
					return err
				}
			case copyMode:
				if info.Mode().IsRegular() {
					sum, err := checksumFile(file.Target)
					if err == nil && file.Checksum != "" && sum == file.Checksum {
						if err := os.Remove(file.Target); err != nil {
							return err
						}
						break
					}
				}
				if _, err := a.backupTarget(file.Target); err != nil {
					return err
				}
			default:
				if _, err := a.backupTarget(file.Target); err != nil {
					return err
				}
			}
		}
		if file.Backup != "" {
			if _, err := os.Stat(file.Backup); err == nil {
				if err := os.MkdirAll(filepath.Dir(file.Target), 0o755); err != nil {
					return err
				}
				if err := os.Rename(file.Backup, file.Target); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *App) configStatus(name string) string {
	lines := a.configStatusLines(name)
	if len(lines) == 0 {
		return fmt.Sprintf("config/%s: unknown", name)
	}
	return lines[0]
}

func (a *App) configStatusLines(name string) []string {
	state, stateErr := a.readConfigState(name)
	manifest, manifestErr := a.loadConfigManifest(name)
	missingDeps := []string{}
	checkLines := []string{}
	if manifestErr == nil {
		for _, dep := range manifest.Dependencies {
			status := dependencyCheckStatusLines(dep)
			checkLines = append(checkLines, status.Lines...)
			if status.State == "missing" {
				missingDeps = append(missingDeps, dep.Name)
			}
		}
	}
	statusLine := ""
	if stateErr != nil {
		if manifestErr != nil {
			return []string{fmt.Sprintf("config/%s: missing-source", name)}
		}
		if len(missingDeps) > 0 {
			statusLine = fmt.Sprintf("config/%s: blocked (missing: %s)", name, strings.Join(missingDeps, ", "))
		} else {
			statusLine = fmt.Sprintf("config/%s: down", name)
		}
		return append([]string{statusLine}, checkLines...)
	}

	stateByTarget := map[string]FileState{}
	for _, file := range state.Files {
		stateByTarget[file.Target] = file
	}
	drifted := false
	partial := false
	if manifestErr == nil {
		for _, mapping := range manifest.Files {
			if !mapping.appliesTo(runtime.GOOS) {
				continue
			}
			target, err := expandPath(mapping.Target)
			if err != nil {
				drifted = true
				continue
			}
			if _, ok := stateByTarget[target]; !ok {
				partial = true
			}
		}
	}

	for _, file := range state.Files {
		info, err := os.Lstat(file.Target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				partial = true
				continue
			}
			drifted = true
			continue
		}
		if file.Mode == linkMode {
			if info.Mode()&os.ModeSymlink == 0 {
				drifted = true
				continue
			}
			linkTarget, err := os.Readlink(file.Target)
			if err != nil || linkTarget != file.Source {
				drifted = true
			}
		}
		if file.Mode == copyMode {
			sum, err := checksumFile(file.Target)
			if err != nil || sum != file.Checksum {
				drifted = true
			}
		}
	}
	if drifted {
		statusLine = fmt.Sprintf("config/%s: drifted", name)
	} else if partial {
		statusLine = fmt.Sprintf("config/%s: partial", name)
	} else if len(missingDeps) > 0 {
		statusLine = fmt.Sprintf("config/%s: blocked (missing: %s)", name, strings.Join(missingDeps, ", "))
	} else {
		statusLine = fmt.Sprintf("config/%s: up (%s)", name, state.Mode)
	}
	return append([]string{statusLine}, checkLines...)
}

func (a *App) upSkill(ref SkillRef, baseDir string) error {
	instances, err := a.expandSkill(ref, baseDir)
	if err != nil {
		return err
	}
	for _, skill := range instances {
		if err := skill.validateSourceTarget(); err != nil {
			return err
		}
		if err := os.MkdirAll(skill.TargetDir, 0o755); err != nil {
			return err
		}
		if info, err := os.Lstat(skill.LinkPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(skill.LinkPath)
				if err == nil && linkTarget == skill.Source {
					if err := a.writeSkillState(skill.state()); err != nil {
						return err
					}
					fmt.Fprintf(a.Out, "skill/%s: linked (%s)\n", skill.Name, skill.TargetKey)
					continue
				}
			}
			return fmt.Errorf("skill/%s: local-conflict at %s", skill.Name, skill.LinkPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Symlink(skill.Source, skill.LinkPath); err != nil {
			return err
		}
		if err := a.writeSkillState(skill.state()); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "skill/%s: linked (%s)\n", skill.Name, skill.TargetKey)
	}
	return nil
}

func (a *App) downSkill(ref SkillRef, baseDir string) error {
	instances, err := a.expandSkill(ref, baseDir)
	if err != nil {
		return err
	}
	for _, skill := range instances {
		state, stateErr := a.readSkillState(skill.TargetKey, skill.Name)
		source := skill.Source
		linkPath := skill.LinkPath
		if stateErr == nil {
			source = state.Source
			linkPath = state.LinkPath
		}
		if info, err := os.Lstat(linkPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(linkPath)
				if err == nil && linkTarget == source {
					if err := os.Remove(linkPath); err != nil {
						return err
					}
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(a.skillStatePath(skill.TargetKey, skill.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintf(a.Out, "skill/%s: down (%s)\n", skill.Name, skill.TargetKey)
	}
	return nil
}

func (a *App) skillStatus(ref SkillRef, baseDir string) []string {
	instances, err := a.expandSkill(ref, baseDir)
	if err != nil {
		return []string{fmt.Sprintf("skill/%s: %v", ref.Name, err)}
	}
	var lines []string
	for _, skill := range instances {
		state, stateErr := a.readSkillState(skill.TargetKey, skill.Name)
		if _, err := os.Stat(skill.Source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				lines = append(lines, fmt.Sprintf("skill/%s:%s: missing-source", skill.Name, skill.TargetKey))
				continue
			}
		}
		info, err := os.Lstat(skill.LinkPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if stateErr == nil {
					lines = append(lines, fmt.Sprintf("skill/%s:%s: drifted", skill.Name, skill.TargetKey))
				} else {
					lines = append(lines, fmt.Sprintf("skill/%s:%s: down", skill.Name, skill.TargetKey))
				}
				continue
			}
			lines = append(lines, fmt.Sprintf("skill/%s:%s: drifted", skill.Name, skill.TargetKey))
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			lines = append(lines, fmt.Sprintf("skill/%s:%s: local-conflict", skill.Name, skill.TargetKey))
			continue
		}
		linkTarget, err := os.Readlink(skill.LinkPath)
		if err != nil {
			lines = append(lines, fmt.Sprintf("skill/%s:%s: drifted", skill.Name, skill.TargetKey))
			continue
		}
		if linkTarget == skill.Source {
			lines = append(lines, fmt.Sprintf("skill/%s:%s: linked", skill.Name, skill.TargetKey))
			continue
		}
		if stateErr == nil && linkTarget != state.Source {
			lines = append(lines, fmt.Sprintf("skill/%s:%s: drifted", skill.Name, skill.TargetKey))
		} else {
			lines = append(lines, fmt.Sprintf("skill/%s:%s: local-conflict", skill.Name, skill.TargetKey))
		}
	}
	return lines
}

type skillInstance struct {
	Name      string
	Source    string
	TargetKey string
	TargetDir string
	LinkPath  string
}

func (skill skillInstance) state() SkillState {
	return SkillState{
		Name:      skill.Name,
		Source:    skill.Source,
		TargetKey: skill.TargetKey,
		TargetDir: skill.TargetDir,
		LinkPath:  skill.LinkPath,
		UpdatedAt: time.Now().UTC(),
	}
}

func (skill skillInstance) validateSourceTarget() error {
	if _, err := os.Stat(filepath.Join(skill.Source, "SKILL.md")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("skill/%s: source missing SKILL.md at %s", skill.Name, skill.Source)
		}
		return err
	}
	sourceClean := filepath.Clean(skill.Source)
	targetClean := filepath.Clean(skill.TargetDir)
	if strings.HasPrefix(targetClean+string(filepath.Separator), sourceClean+string(filepath.Separator)) {
		return fmt.Errorf("skill/%s: target %s is inside source %s", skill.Name, skill.TargetDir, skill.Source)
	}
	return nil
}

func (a *App) expandSkill(ref SkillRef, baseDir string) ([]skillInstance, error) {
	if ref.Name == "" {
		return nil, errors.New("skill name is required")
	}
	if ref.Source == "" {
		return nil, fmt.Errorf("skill/%s: source is required", ref.Name)
	}
	if len(ref.Targets) == 0 {
		return nil, fmt.Errorf("skill/%s: at least one target is required", ref.Name)
	}
	sourceRootAbs, err := expandPathRelative(ref.Source, baseDir)
	if err != nil {
		return nil, err
	}
	sourcePath := filepath.Join(sourceRootAbs, ref.Name)
	var instances []skillInstance
	for _, target := range ref.Targets {
		targetPath := strings.TrimSpace(target)
		if targetPath == "" {
			return nil, fmt.Errorf("skill/%s: target path is required", ref.Name)
		}
		targetDir, err := expandPathRelative(targetPath, baseDir)
		if err != nil {
			return nil, err
		}
		instances = append(instances, skillInstance{
			Name:      ref.Name,
			Source:    sourcePath,
			TargetKey: targetPath,
			TargetDir: targetDir,
			LinkPath:  filepath.Join(targetDir, ref.Name),
		})
	}
	return instances, nil
}

func (a *App) runPull() error {
	if err := ensureGit(a.Out); err != nil {
		return err
	}
	if err := a.ensureSource(); err != nil {
		return err
	}
	if err := runCommand("git", "-C", a.sourceDir(), "pull", "--ff-only"); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "source: pulled %s\n", a.sourceDir())
	return nil
}

func (a *App) runUpdate() error {
	assetName := fmt.Sprintf("yc1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archiveURL, checksumURL, err := latestAssetURLs(assetName)
	if err != nil {
		return err
	}
	archive, err := downloadBytes(archiveURL)
	if err != nil {
		return err
	}
	checksumBytes, err := downloadBytes(checksumURL)
	if err != nil {
		return err
	}
	expected := strings.Fields(string(checksumBytes))
	if len(expected) == 0 {
		return fmt.Errorf("empty checksum for %s", assetName)
	}
	actualHash := sha256.Sum256(archive)
	if !strings.EqualFold(expected[0], hex.EncodeToString(actualHash[:])) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	binary, err := extractYc1Binary(archive)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", exe, os.Getpid())
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	fmt.Fprintf(a.Out, "yc1: updated to latest release\n")
	return nil
}

func (a *App) ensureSource() error {
	if a.Root == "" {
		return errors.New("could not determine yc1 root")
	}
	source := a.sourceDir()
	if _, err := os.Stat(filepath.Join(source, ".git")); err == nil {
		return nil
	}
	if entries, err := os.ReadDir(source); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists but is not a git repository", source)
	}
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		return err
	}
	if err := ensureGit(a.Out); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "source: cloning %s into %s\n", a.RepoURL, source)
	return runCommand("git", "clone", a.RepoURL, source)
}

func ensureGit(out io.Writer) error {
	if _, err := exec.LookPath("git"); err == nil {
		return nil
	}
	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "brew install git"
	case "linux":
		command = "sudo apt-get update && sudo apt-get install -y git"
	default:
		return fmt.Errorf("git is required and has no %s installer", runtime.GOOS)
	}
	fmt.Fprintln(out, "source: installing dependency git")
	return runShell(command)
}

func (a *App) readConfigState(name string) (ConfigState, error) {
	data, err := os.ReadFile(a.configStatePath(name))
	if err != nil {
		return ConfigState{}, err
	}
	var state ConfigState
	if err := json.Unmarshal(data, &state); err != nil {
		return ConfigState{}, err
	}
	return state, nil
}

func (a *App) writeConfigState(state ConfigState) error {
	if err := os.MkdirAll(a.configStateDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(a.configStatePath(state.Config), data, 0o644)
}

func (a *App) readSkillState(targetKey, name string) (SkillState, error) {
	data, err := os.ReadFile(a.skillStatePath(targetKey, name))
	if err != nil {
		return SkillState{}, err
	}
	var state SkillState
	if err := json.Unmarshal(data, &state); err != nil {
		return SkillState{}, err
	}
	return state, nil
}

func (a *App) writeSkillState(state SkillState) error {
	if err := os.MkdirAll(filepath.Dir(a.skillStatePath(state.TargetKey, state.Name)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(a.skillStatePath(state.TargetKey, state.Name), data, 0o644)
}

func (a *App) sourceDir() string {
	return filepath.Join(a.Root, "source")
}

func (a *App) configsDir() string {
	return filepath.Join(a.sourceDir(), "configs")
}

func (a *App) profilesDir() string {
	return filepath.Join(a.sourceDir(), "profiles")
}

func (a *App) localProfilesDir() string {
	return filepath.Join(a.sourceDir(), "_profiles")
}

func (a *App) stateDir() string {
	return filepath.Join(a.Root, "state")
}

func (a *App) configStateDir() string {
	return filepath.Join(a.stateDir(), "configs")
}

func (a *App) configStatePath(name string) string {
	return filepath.Join(a.configStateDir(), name+".json")
}

func (a *App) skillStatePath(targetKey, name string) string {
	return filepath.Join(a.stateDir(), "skills", sanitizeStateKey(targetKey), name+".json")
}

func expandPath(path string) (string, error) {
	return expandPathRelative(path, ".")
}

func expandPathRelative(path, baseDir string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}
	path = os.ExpandEnv(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Abs(path)
}

func sanitizePath(path string) string {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, string(filepath.Separator))
	replacer := strings.NewReplacer(string(filepath.Separator), "__", ":", "_")
	return replacer.Replace(path)
}

func sanitizeStateKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "default"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "~", "home")
	return replacer.Replace(key)
}

func checksumFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runShell(command string) error {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

type releaseResponse struct {
	Assets []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func latestAssetURLs(assetName string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "yc1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("GitHub release lookup failed: %s", resp.Status)
	}
	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	return assetURLsFromRelease(release, assetName)
}

func assetURLsFromRelease(release releaseResponse, assetName string) (string, string, error) {
	var archiveURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			archiveURL = asset.BrowserDownloadURL
		case assetName + ".sha256":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if archiveURL == "" {
		return "", "", fmt.Errorf("release asset %s not found", assetName)
	}
	if checksumURL == "" {
		return "", "", fmt.Errorf("release checksum %s.sha256 not found", assetName)
	}
	return archiveURL, checksumURL, nil
}

func downloadBytes(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "yc1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed for %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func extractYc1Binary(archive []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(header.Name) != "yc1" {
			continue
		}
		return io.ReadAll(tarReader)
	}
	return nil, errors.New("yc1 binary not found in archive")
}
