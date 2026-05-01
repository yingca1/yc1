package yc1

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigUpStatusDownLinkRestoresBackup(t *testing.T) {
	app, home := newTestApp(t)
	writeTestConfig(t, app.Root, "demo", "demo.conf", "~/.demorc", "managed\n")
	target := filepath.Join(home, ".demorc")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := app.Run([]string{"config", "up", "demo"}); code != 0 {
		t.Fatalf("config up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	linkTarget, err := os.Readlink(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(linkTarget, filepath.Join("configs", "demo", "demo.conf")) {
		t.Fatalf("unexpected link target %q", linkTarget)
	}

	app.Out.(*bytes.Buffer).Reset()
	if code := app.Run([]string{"config", "status", "demo"}); code != 0 {
		t.Fatalf("config status returned %d", code)
	}
	if got := app.Out.(*bytes.Buffer).String(); !strings.Contains(got, "config/demo: up (link)") {
		t.Fatalf("unexpected status %q", got)
	}

	if code := app.Run([]string{"config", "down", "demo"}); code != 0 {
		t.Fatalf("config down returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Fatalf("backup was not restored: %q", string(data))
	}
}

func TestConfigCopyModeDetectsDriftAndDownBacksUpChangedFile(t *testing.T) {
	app, home := newTestApp(t)
	writeTestConfig(t, app.Root, "demo", "demo.conf", "~/.demorc", "managed\n")
	target := filepath.Join(home, ".demorc")

	if code := app.Run([]string{"config", "up", "demo", "--copy"}); code != 0 {
		t.Fatalf("config up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if err := os.WriteFile(target, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app.Out.(*bytes.Buffer).Reset()
	if code := app.Run([]string{"config", "status", "demo"}); code != 0 {
		t.Fatalf("status returned %d", code)
	}
	if got := app.Out.(*bytes.Buffer).String(); !strings.Contains(got, "config/demo: drifted") {
		t.Fatalf("unexpected status %q", got)
	}

	if code := app.Run([]string{"config", "down", "demo"}); code != 0 {
		t.Fatalf("config down returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target to be removed, stat err=%v", err)
	}
	backups, err := filepath.Glob(filepath.Join(app.Root, "backups", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("expected drifted copy to be backed up")
	}
}

func TestConfigUpClonesSourceWhenMissing(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	root := filepath.Join(temp, "root")
	bin := filepath.Join(temp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGit := filepath.Join(bin, "git")
	script := `#!/bin/sh
set -eu
if [ "$1" != "clone" ]; then
  exit 2
fi
dest="$3"
mkdir -p "$dest/.git" "$dest/configs/demo"
cat > "$dest/configs/demo/demo.conf" <<'EOF'
managed
EOF
cat > "$dest/configs/demo/yc1.yml" <<'EOF'
name: demo
files:
  - source: demo.conf
    target: ~/.demorc
EOF
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("YC1_ROOT", root)
	t.Setenv("YC1_REPO_URL", "https://example.invalid/repo.git")

	app := NewApp("test", &bytes.Buffer{}, &bytes.Buffer{})
	if code := app.Run([]string{"config", "up", "demo"}); code != 0 {
		t.Fatalf("config up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Stat(filepath.Join(root, "source", ".git")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".demorc")); err != nil {
		t.Fatal(err)
	}
}

func TestTopLevelRequiresProfileWhenNoLocalFile(t *testing.T) {
	app, _ := newTestApp(t)
	cwd := t.TempDir()
	withCwd(t, cwd)

	if code := app.Run([]string{"status"}); code == 0 {
		t.Fatalf("expected missing profile to fail")
	}
	if got := app.Err.(*bytes.Buffer).String(); !strings.Contains(got, "yc1.yml not found") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestProfileFileRunsConfigAndSkill(t *testing.T) {
	app, home := newTestApp(t)
	writeTestConfig(t, app.Root, "demo", "demo.conf", "~/.demorc", "managed\n")

	profileDir := t.TempDir()
	skillSource := filepath.Join(profileDir, "source-skills")
	writeTestSkill(t, skillSource, "demo-skill")
	profilePath := filepath.Join(profileDir, "yc1.yml")
	writeFile(t, profilePath, `version: 1
configs:
  - demo
skills:
  - name: demo-skill
    source: ./source-skills
    targets:
      - .agents/skills
`)

	if code := app.Run([]string{"up", "-f", profilePath}); code != 0 {
		t.Fatalf("profile up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Lstat(filepath.Join(home, ".demorc")); err != nil {
		t.Fatal(err)
	}
	skillLink := filepath.Join(profileDir, ".agents", "skills", "demo-skill")
	linkTarget, err := os.Readlink(skillLink)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != filepath.Join(skillSource, "demo-skill") {
		t.Fatalf("unexpected skill link target %q", linkTarget)
	}

	app.Out.(*bytes.Buffer).Reset()
	if code := app.Run([]string{"status", "-f", profilePath}); code != 0 {
		t.Fatalf("profile status returned %d", code)
	}
	got := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(got, "config/demo: up (link)") || !strings.Contains(got, "skill/demo-skill:.agents/skills: linked") {
		t.Fatalf("unexpected profile status:\n%s", got)
	}

	if code := app.Run([]string{"down", "-f", profilePath}); code != 0 {
		t.Fatalf("profile down returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Lstat(skillLink); !os.IsNotExist(err) {
		t.Fatalf("expected skill link removed, err=%v", err)
	}
}

func TestProfileNameLoadsTrackedProfileFile(t *testing.T) {
	app, _ := newTestApp(t)
	writeFile(t, filepath.Join(app.Root, "source", "profiles", "default", "yc1.yml"), `version: 1
configs:
  - git
`)

	selection, err := app.selectProfile(actionOptions{ProfileName: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(selection.Profile.Configs, ",") != "git" {
		t.Fatalf("unexpected tracked profile: %#v", selection.Profile.Configs)
	}
	wantBaseDir := filepath.Join(app.Root, "source", "profiles", "default")
	if selection.BaseDir != wantBaseDir {
		t.Fatalf("unexpected profile base dir: %q", selection.BaseDir)
	}
}

func TestProfileNamePrefersLocalProfileOverride(t *testing.T) {
	app, _ := newTestApp(t)
	writeFile(t, filepath.Join(app.Root, "source", "profiles", "default", "yc1.yml"), `version: 1
configs:
  - git
`)
	writeFile(t, filepath.Join(app.Root, "source", "_profiles", "default", "yc1.yml"), `version: 1
configs:
  - curl
`)

	selection, err := app.selectProfile(actionOptions{ProfileName: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(selection.Profile.Configs, ",") != "curl" {
		t.Fatalf("unexpected local override profile: %#v", selection.Profile.Configs)
	}
	wantBaseDir := filepath.Join(app.Root, "source", "_profiles", "default")
	if selection.BaseDir != wantBaseDir {
		t.Fatalf("unexpected profile base dir: %q", selection.BaseDir)
	}
}

func TestProfileNameFallsBackToHardcodedDefaults(t *testing.T) {
	app, _ := newTestApp(t)

	selection, err := app.selectProfile(actionOptions{ProfileName: "minimal"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(selection.Profile.Configs, ",") != "git,curl,wget" {
		t.Fatalf("unexpected minimal profile: %#v", selection.Profile.Configs)
	}
}

func TestProfileNameMissingReportsUnknownProfile(t *testing.T) {
	app, _ := newTestApp(t)

	_, err := app.selectProfile(actionOptions{ProfileName: "missing"})
	if err == nil {
		t.Fatal("expected unknown profile error")
	}
	if !strings.Contains(err.Error(), `unknown profile "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillCommandReadsCurrentDirectoryProfile(t *testing.T) {
	app, _ := newTestApp(t)
	profileDir := t.TempDir()
	skillSource := filepath.Join(profileDir, "source-skills")
	writeTestSkill(t, skillSource, "demo-skill")
	writeTestSkill(t, skillSource, "other-skill")
	writeFile(t, filepath.Join(profileDir, "yc1.yml"), `version: 1
skills:
  - name: demo-skill
    source: ./source-skills
    targets:
      - .agents/skills
  - name: other-skill
    source: ./source-skills
    targets:
      - .agents/skills
`)
	withCwd(t, profileDir)

	if code := app.Run([]string{"skill", "up", "demo-skill"}); code != 0 {
		t.Fatalf("skill up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Lstat(filepath.Join(profileDir, ".agents", "skills", "demo-skill")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, ".agents", "skills", "other-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected other-skill to remain down, err=%v", err)
	}

	app.Out.(*bytes.Buffer).Reset()
	if code := app.Run([]string{"skill", "status"}); code != 0 {
		t.Fatalf("skill status returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	got := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(got, "skill/demo-skill:.agents/skills: linked") {
		t.Fatalf("unexpected status:\n%s", got)
	}
	if !strings.Contains(got, "skill/other-skill:.agents/skills: down") {
		t.Fatalf("unexpected status:\n%s", got)
	}
}

func TestProfileVarsExpandSkillSourceAndTargetPaths(t *testing.T) {
	app, _ := newTestApp(t)
	profileDir := t.TempDir()
	skillSource := filepath.Join(profileDir, "source-skills")
	writeTestSkill(t, skillSource, "demo-skill")
	writeFile(t, filepath.Join(profileDir, "yc1.yml"), `version: 1
vars:
  project_root: .
  skill_root: ${vars.project_root}/source-skills
  agents_target: .agents/skills
skills:
  - name: demo-skill
    source: ${vars.skill_root}
    targets:
      - ${vars.agents_target}
`)
	withCwd(t, profileDir)

	if code := app.Run([]string{"skill", "up", "demo-skill"}); code != 0 {
		t.Fatalf("skill up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	linkTarget, err := os.Readlink(filepath.Join(profileDir, ".agents", "skills", "demo-skill"))
	if err != nil {
		t.Fatal(err)
	}
	actualTarget, err := filepath.EvalSymlinks(linkTarget)
	if err != nil {
		t.Fatal(err)
	}
	wantTarget, err := filepath.EvalSymlinks(filepath.Join(profileDir, "source-skills", "demo-skill"))
	if err != nil {
		t.Fatal(err)
	}
	if actualTarget != wantTarget {
		t.Fatalf("unexpected skill link target %q", linkTarget)
	}
}

func TestProfileVarsRejectUnknownAndCircularReferences(t *testing.T) {
	unknownPath := filepath.Join(t.TempDir(), "unknown.yml")
	writeFile(t, unknownPath, `version: 1
skills:
  - name: demo-skill
    source: ${vars.missing}
    targets:
      - .agents/skills
`)
	_, err := readProfileFile(unknownPath)
	if err == nil {
		t.Fatal("expected unknown variable to fail")
	}
	if !strings.Contains(err.Error(), `unknown profile variable "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}

	circularPath := filepath.Join(t.TempDir(), "circular.yml")
	writeFile(t, circularPath, `version: 1
vars:
  a: ${vars.b}
  b: ${vars.a}
`)
	_, err = readProfileFile(circularPath)
	if err == nil {
		t.Fatal("expected circular variable to fail")
	}
	if !strings.Contains(err.Error(), "circular reference") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillCommandRequiresCurrentDirectoryProfile(t *testing.T) {
	app, _ := newTestApp(t)
	withCwd(t, t.TempDir())

	if code := app.Run([]string{"skill", "status"}); code == 0 {
		t.Fatal("expected missing yc1.yml to fail")
	}
	if got := app.Err.(*bytes.Buffer).String(); !strings.Contains(got, "yc1.yml not found") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestSkillCommandRejectsUnknownActionBeforeReadingProfile(t *testing.T) {
	app, _ := newTestApp(t)
	withCwd(t, t.TempDir())

	if code := app.Run([]string{"skill", "restart"}); code == 0 {
		t.Fatal("expected unknown skill command to fail")
	}
	if got := app.Err.(*bytes.Buffer).String(); !strings.Contains(got, `unknown skill command "restart"`) {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestSkillRequiresExplicitSource(t *testing.T) {
	app, _ := newTestApp(t)
	_, err := app.expandSkill(SkillRef{
		Name:    "demo-skill",
		Targets: []string{".agents/skills"},
	}, ".")
	if err == nil {
		t.Fatal("expected missing source to fail")
	}
	if !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillRequiresExplicitTargets(t *testing.T) {
	app, _ := newTestApp(t)
	_, err := app.expandSkill(SkillRef{Name: "demo-skill", Source: "./source-skills"}, ".")
	if err == nil {
		t.Fatal("expected missing targets to fail")
	}
	if !strings.Contains(err.Error(), "at least one target is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillTargetRequiresPath(t *testing.T) {
	app, _ := newTestApp(t)
	_, err := app.expandSkill(SkillRef{
		Name:    "demo-skill",
		Source:  "./source-skills",
		Targets: []string{""},
	}, ".")
	if err == nil {
		t.Fatal("expected missing target path to fail")
	}
	if !strings.Contains(err.Error(), "target path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillTargetStringIsTreatedAsPath(t *testing.T) {
	app, _ := newTestApp(t)
	profileDir := t.TempDir()
	skillSource := filepath.Join(profileDir, "source-skills")
	writeTestSkill(t, skillSource, "demo-skill")
	writeFile(t, filepath.Join(profileDir, "yc1.yml"), `version: 1
skills:
  - name: demo-skill
    source: ./source-skills
    targets: [agents-project]
`)
	withCwd(t, profileDir)

	if code := app.Run([]string{"skill", "up", "demo-skill"}); code != 0 {
		t.Fatalf("skill up returned %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "agents-project", "demo-skill")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, ".agents", "skills", "demo-skill")); !os.IsNotExist(err) {
		t.Fatalf("target was treated like an alias, err=%v", err)
	}
}

func TestSkillLocalConflict(t *testing.T) {
	app, _ := newTestApp(t)
	profileDir := t.TempDir()
	skillSource := filepath.Join(profileDir, "source-skills")
	writeTestSkill(t, skillSource, "demo-skill")
	conflict := filepath.Join(profileDir, ".agents", "skills", "demo-skill")
	if err := os.MkdirAll(conflict, 0o755); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(profileDir, "yc1.yml")
	writeFile(t, profilePath, `version: 1
skills:
  - name: demo-skill
    source: ./source-skills
    targets:
      - .agents/skills
`)

	if code := app.Run([]string{"up", "-f", profilePath}); code == 0 {
		t.Fatalf("expected local conflict to fail")
	}
	if got := app.Err.(*bytes.Buffer).String(); !strings.Contains(got, "local-conflict") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestRepositoryConfigManifestsParse(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	manifestPaths, err := filepath.Glob(filepath.Join(repoRoot, "configs", "*", "yc1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifestPaths) == 0 {
		t.Fatal("expected repository config manifests")
	}
	for _, manifestPath := range manifestPaths {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		var manifest ConfigManifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("%s: %v", manifestPath, err)
		}
		if manifest.Name == "" {
			t.Fatalf("%s: missing name", manifestPath)
		}
		configDir := filepath.Dir(manifestPath)
		for _, mapping := range manifest.Files {
			if mapping.Source == "" || mapping.Target == "" {
				t.Fatalf("%s: invalid file mapping", manifestPath)
			}
			if _, err := os.Stat(filepath.Join(configDir, mapping.Source)); err != nil {
				t.Fatalf("%s: missing source %s: %v", manifestPath, mapping.Source, err)
			}
		}
		for _, dep := range manifest.Dependencies {
			if dep.Name == "" {
				t.Fatalf("%s: dependency missing name", manifestPath)
			}
			if dep.Install[runtime.GOOS] == "" {
				t.Fatalf("%s: dependency %s missing %s installer", manifestPath, dep.Name, runtime.GOOS)
			}
		}
	}
}

func TestRepositoryProfileFilesParse(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	profilePaths, err := filepath.Glob(filepath.Join(repoRoot, "profiles", "*", "yc1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profilePaths) == 0 {
		t.Fatal("expected repository profile files")
	}
	for _, profilePath := range profilePaths {
		profile, err := readProfileFile(profilePath)
		if err != nil {
			t.Fatalf("%s: %v", profilePath, err)
		}
		if profile.Version != 1 {
			t.Fatalf("%s: unexpected version %d", profilePath, profile.Version)
		}
		if len(profile.Configs) == 0 && len(profile.Skills) == 0 {
			t.Fatalf("%s: expected configs or skills", profilePath)
		}
	}
}

func TestDemoProfileParses(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	profile, err := readProfileFile(filepath.Join(repoRoot, "profiles", "skills-demo", "yc1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Skills) == 0 {
		t.Fatal("expected demo profile skills")
	}
	for _, skill := range profile.Skills {
		if skill.Name == "" || skill.Source == "" || len(skill.Targets) == 0 {
			t.Fatalf("invalid demo skill entry: %#v", skill)
		}
		for _, target := range skill.Targets {
			if target == "" {
				t.Fatalf("invalid demo skill target: %#v", target)
			}
		}
	}
}

func TestLocalProfilesDirectoryIsIgnored(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n/_profiles/\n") && !strings.Contains(string(data), "\n_profiles/\n") {
		t.Fatal("expected .gitignore to ignore _profiles/")
	}
}

func TestAssetURLsFromRelease(t *testing.T) {
	release := releaseResponse{Assets: []releaseAsset{
		{Name: "yc1_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.test/archive"},
		{Name: "yc1_darwin_arm64.tar.gz.sha256", BrowserDownloadURL: "https://example.test/checksum"},
	}}
	archive, checksum, err := assetURLsFromRelease(release, "yc1_darwin_arm64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if archive != "https://example.test/archive" || checksum != "https://example.test/checksum" {
		t.Fatalf("unexpected URLs: %q %q", archive, checksum)
	}
}

func TestExtractYc1Binary(t *testing.T) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("binary")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "yc1", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(tarWriter, bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractYc1Binary(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("unexpected binary content %q", string(got))
	}
}

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	root := filepath.Join(temp, "root")
	if err := os.MkdirAll(filepath.Join(root, "source", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("YC1_ROOT", root)
	app := NewApp("test", &bytes.Buffer{}, &bytes.Buffer{})
	return app, home
}

func writeTestConfig(t *testing.T, root, name, source, target, content string) {
	t.Helper()
	configDir := filepath.Join(root, "source", "configs", name)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, source), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `name: ` + name + `
files:
  - source: ` + source + `
    target: ` + target + `
    os:
      - ` + runtime.GOOS + `
`
	if err := os.WriteFile(filepath.Join(configDir, "yc1.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestSkill(t *testing.T, sourceRoot, name string) {
	t.Helper()
	dir := filepath.Join(sourceRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withCwd(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}
