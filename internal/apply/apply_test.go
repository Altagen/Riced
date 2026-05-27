package apply_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Altagen/Riced/internal/apply"
	"github.com/Altagen/Riced/internal/manifest"
)

// buildArtifact creates one file (with the given content) under buildDir
// at the given relative path. Returns the absolute path for convenience.
func buildArtifact(t *testing.T, buildDir, rel, content string) string {
	t.Helper()
	p := filepath.Join(buildDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeApplyEnvironment is the common setup for apply tests: a temp HOME,
// a temp build dir simulating what `riced generate` would have produced
// for slug s4-test, and a fake KDE adapter.
type fakeApplyEnvironment struct {
	Home     string
	BuildDir string
	Manifest *manifest.Manifest
	KDE      *apply.FakeKDE
	Now      time.Time
}

func setupFakeApply(t *testing.T) *fakeApplyEnvironment {
	t.Helper()
	// Force the XDG fallbacks to HomeDir-based defaults, otherwise a CI
	// runner whose env carries XDG_*_HOME would route writes outside the
	// test's temp tree (caught the hard way on the initial main push).
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	home := t.TempDir()
	buildDir := filepath.Join(t.TempDir(), "s4-test")

	// Minimal generated output. We only need files whose Targets.Map
	// recognizes them -- anything else is ignored by the planner.
	buildArtifact(t, buildDir, "s4-test.colors", "[General]\nName=S4 Test\n")
	buildArtifact(t, buildDir, "konsole/s4-test.colorscheme", "[Background]\nColor=0,0,0\n")
	buildArtifact(t, buildDir, "konsole/s4-test.profile", "[General]\nName=S4 Test\n")
	buildArtifact(t, buildDir, "gtk-3.0/gtk.css", "@define-color accent #ff0000;\n")
	buildArtifact(t, buildDir, "gtk-4.0/gtk.css", "@define-color accent #ff0000;\n")
	// One wallpaper symlink target.
	wallpaperSrc := filepath.Join(t.TempDir(), "src.png")
	if err := os.WriteFile(wallpaperSrc, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	wallpapersDir := filepath.Join(buildDir, "wallpapers")
	if err := os.MkdirAll(wallpapersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wallpaperSrc, filepath.Join(wallpapersDir, "01-pic.png")); err != nil {
		t.Fatal(err)
	}

	m := &manifest.Manifest{
		Meta: manifest.Meta{Name: "S4 Test", Slug: "s4-test"},
		Wallpapers: manifest.Wallpapers{
			Mode:  "slideshow",
			Paths: []string{filepath.Join(wallpapersDir, "01-pic.png")},
		},
	}
	return &fakeApplyEnvironment{
		Home:     home,
		BuildDir: buildDir,
		Manifest: m,
		KDE:      &apply.FakeKDE{},
		Now:      time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	}
}

func TestApply_CleanInstall(t *testing.T) {
	env := setupFakeApply(t)

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Every mapped artifact must now exist under HOME.
	want := []string{
		filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors"),
		filepath.Join(env.Home, ".local/share/konsole/s4-test.colorscheme"),
		filepath.Join(env.Home, ".local/share/konsole/s4-test.profile"),
		filepath.Join(env.Home, ".config/gtk-3.0/gtk.css"),
		filepath.Join(env.Home, ".config/gtk-4.0/gtk.css"),
		filepath.Join(env.Home, ".local/share/riced/wallpapers/s4-test/01-pic.png"),
	}
	for _, p := range want {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	// State must be recorded.
	state, err := apply.LoadState(apply.StatePath(env.Home))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state == nil {
		t.Fatal("state file not created")
	}
	if state.Slug != "s4-test" {
		t.Errorf("state.Slug = %q, want s4-test", state.Slug)
	}
	if len(state.WrittenAt) != len(want) {
		t.Errorf("state.WrittenAt = %d entries, want %d", len(state.WrittenAt), len(want))
	}

	// KDE side effects must have been invoked.
	expectedCalls := []string{
		"ApplyColorScheme:s4-test",
		"ReconfigureKWin",
	}
	sort.Strings(env.KDE.Calls)
	sort.Strings(expectedCalls)
	for _, ec := range expectedCalls {
		found := false
		for _, c := range env.KDE.Calls {
			if c == ec {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing KDE call %q (got %v)", ec, env.KDE.Calls)
		}
	}
}

func TestApply_BacksUpExisting(t *testing.T) {
	env := setupFakeApply(t)

	// Pre-create a gtk.css to verify backup behavior.
	existing := filepath.Join(env.Home, ".config/gtk-4.0/gtk.css")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("/* original */\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.Execute(plan, apply.ExecOptions{KDE: env.KDE, Now: func() time.Time { return env.Now }}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The original gtk.css must have been copied into the backup dir at
	// its mirrored path under BackupRoot.
	state, _ := apply.LoadState(apply.StatePath(env.Home))
	if state.BackupRoot == "" {
		t.Fatal("expected BackupRoot to be set")
	}
	backedUp := filepath.Join(state.BackupRoot, strings.TrimPrefix(existing, "/"))
	body, err := os.ReadFile(backedUp)
	if err != nil {
		t.Fatalf("backup not found at %s: %v", backedUp, err)
	}
	if string(body) != "/* original */\n" {
		t.Errorf("backup contents not preserved: %q", body)
	}

	// The live file should now contain the new content.
	body, _ = os.ReadFile(existing)
	if !strings.Contains(string(body), "accent") {
		t.Errorf("live gtk.css was not overwritten: %q", body)
	}
}

func TestRevert_RestoresOriginal(t *testing.T) {
	env := setupFakeApply(t)

	// Pre-existing live file we expect to see restored verbatim.
	existing := filepath.Join(env.Home, ".config/gtk-3.0/gtk.css")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "/* hand-crafted, untouched */\n"
	if err := os.WriteFile(existing, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err := apply.Execute(plan, apply.ExecOptions{KDE: env.KDE, Now: func() time.Time { return env.Now }}); err != nil {
		t.Fatal(err)
	}

	// Now revert.
	if err := apply.Revert(env.Home); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	// Pre-existing file restored to its original content.
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("expected %s to still exist after revert: %v", existing, err)
	}
	if string(got) != original {
		t.Errorf("restored content = %q, want %q", got, original)
	}

	// A Riced-only file (no prior backup) must be removed.
	cleanRm := filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors")
	if _, err := os.Lstat(cleanRm); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed by revert, got err = %v", cleanRm, err)
	}

	// State file gone.
	if _, err := os.Lstat(apply.StatePath(env.Home)); !os.IsNotExist(err) {
		t.Errorf("state file should be deleted by revert")
	}
}

func TestRevert_NoStateIsNotAnError(t *testing.T) {
	tmp := t.TempDir()
	if err := apply.Revert(tmp); err == nil {
		t.Fatal("Revert on missing state should report an error")
	}
}

// TestRevert_AggregatesRemoveFailures verifies the M2 fix: if a path in
// State.WrittenAt cannot be removed (here: it's a non-empty directory,
// which os.Remove refuses), the error surfaces as a final aggregated
// error instead of being silently swallowed.
func TestRevert_AggregatesRemoveFailures(t *testing.T) {
	home := t.TempDir()
	// Stage a directory with content where state thinks a file is. Calling
	// os.Remove on a non-empty dir returns syscall.ENOTEMPTY, which is
	// exactly the "real" remove failure we want to test.
	stuck := filepath.Join(home, ".local/share/color-schemes/wedged.colors")
	if err := os.MkdirAll(stuck, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := &apply.State{
		SchemaVersion: 1,
		Slug:          "wedged",
		AppliedAt:     time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
		WrittenAt:     []string{stuck},
	}
	if err := state.Save(apply.StatePath(home)); err != nil {
		t.Fatal(err)
	}

	err := apply.Revert(home)
	if err == nil {
		t.Fatal("Revert should report the unremovable entry")
	}
	if !strings.Contains(err.Error(), "unremovable") {
		t.Errorf("error message should mention 'unremovable', got: %v", err)
	}

	// State file should NOT have been deleted -- the apply is still
	// "active" from Riced's POV, and a subsequent revert can retry once
	// the user has unjammed things.
	if _, err := os.Stat(apply.StatePath(home)); err != nil {
		t.Errorf("state file should survive a failed revert: %v", err)
	}
}

func TestApply_LauncherIcon(t *testing.T) {
	env := setupFakeApply(t)

	// Drop a pretend launcher icon symlink into the build tree the way
	// materializeLauncherIcon would. Use a regular file as the symlink
	// target so EvalSymlinks-style operations downstream don't trip.
	iconTarget := filepath.Join(t.TempDir(), "s4-icon.png")
	if err := os.WriteFile(iconTarget, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	iconsDir := filepath.Join(env.BuildDir, "icons")
	if err := os.MkdirAll(iconsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(iconTarget, filepath.Join(iconsDir, "launcher.png")); err != nil {
		t.Fatal(err)
	}

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Plan must contain (a) a symlink action into ~/.local/share/riced/icons/
	// and (b) a set-launcher-icon kde action pointing at the same path.
	wantIconDst := filepath.Join(env.Home, ".local/share/riced/icons/s4-test-launcher.png")
	var foundSymlink, foundKDE bool
	for _, a := range plan.Actions {
		if a.Kind == "symlink" && a.Dst == wantIconDst {
			foundSymlink = true
		}
		if a.Kind == "kde" && a.Src == "set-launcher-icon "+wantIconDst {
			foundKDE = true
		}
	}
	if !foundSymlink {
		t.Errorf("missing symlink action to %s; plan was:\n%s", wantIconDst, plan.Format())
	}
	if !foundKDE {
		t.Errorf("missing set-launcher-icon kde action; plan was:\n%s", plan.Format())
	}

	// Execute and confirm the FakeKDE saw the icon path.
	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "SetLauncherIcon:" + wantIconDst
	found := false
	for _, c := range env.KDE.Calls {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FakeKDE.Calls missing %q\ngot: %v", want, env.KDE.Calls)
	}
}

// TestApply_PerScreenSlideshow verifies that [[wallpapers.screens]]
// produces one set-wallpaper-slideshow action per declared screen, each
// targeting its slug-scoped screen-N subdirectory. Also covers the
// "ghost screen" robustness case (a manifest declares screen 5 on a
// 2-screen machine): the plan still emits the action, and the JS at
// runtime is the one that silently skips when no desktop matches.
func TestApply_PerScreenSlideshow(t *testing.T) {
	env := setupFakeApply(t)

	// Override the manifest's wallpapers to use per-screen lists. We
	// reuse the existing wallpaper symlink target from setupFakeApply.
	zero, five := 0, 5
	vertical := "vertical"
	star := "*"
	env.Manifest.Wallpapers = manifest.Wallpapers{
		Mode:     "slideshow",
		Interval: 600,
		Screens: []manifest.WallpapersScreen{
			{Index: &zero, Paths: env.Manifest.Wallpapers.Paths}, // most specific
			{Orientation: vertical, Paths: env.Manifest.Wallpapers.Paths},
			{Index: &five, Paths: env.Manifest.Wallpapers.Paths}, // ghost: no such screen IRL
			{Match: star, Paths: env.Manifest.Wallpapers.Paths},  // catch-all
		},
	}

	// Re-materialize wallpapers into criterion-named subdirs.
	wpRoot := filepath.Join(env.BuildDir, "wallpapers")
	subdirs := []string{"screen-0", "vertical", "screen-5", "default"}
	for _, sub := range subdirs {
		dir := filepath.Join(wpRoot, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), sub+".png")
		if err := os.WriteFile(target, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "01-pic.png")); err != nil {
			t.Fatal(err)
		}
	}
	// Remove the flat wallpaper symlink so it doesn't get picked up by the walk.
	if err := os.Remove(filepath.Join(wpRoot, "01-pic.png")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Expect ONE slideshow action carrying every rule in manifest order.
	wpDataRoot := filepath.Join(env.Home, ".local/share/riced/wallpapers/s4-test")
	wantArgs := []string{
		"idx:0:" + filepath.Join(wpDataRoot, "screen-0"),
		"orient:vertical:" + filepath.Join(wpDataRoot, "vertical"),
		"idx:5:" + filepath.Join(wpDataRoot, "screen-5"),
		"match:*:" + filepath.Join(wpDataRoot, "default"),
	}
	var found *apply.Action
	for i, a := range plan.Actions {
		if a.Src == "set-wallpaper-slideshow 600" {
			found = &plan.Actions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no set-wallpaper-slideshow action emitted; plan:\n%s", plan.Format())
	}
	if !slices.Equal(found.Args, wantArgs) {
		t.Errorf("args mismatch\n  got:  %v\n  want: %v", found.Args, wantArgs)
	}

	// Execute against the fake and confirm the rule list reaches the adapter
	// intact (kind=value=>path triples joined with commas).
	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantCall := "SetWallpaperSlideshow:[" +
		"idx=0=>" + filepath.Join(wpDataRoot, "screen-0") + "," +
		"orient=vertical=>" + filepath.Join(wpDataRoot, "vertical") + "," +
		"idx=5=>" + filepath.Join(wpDataRoot, "screen-5") + "," +
		"match=*=>" + filepath.Join(wpDataRoot, "default") +
		"]|600"
	gotCall := ""
	for _, c := range env.KDE.Calls {
		if strings.HasPrefix(c, "SetWallpaperSlideshow:") {
			gotCall = c
			break
		}
	}
	if gotCall != wantCall {
		t.Errorf("FakeKDE call mismatch\n  got:  %q\n  want: %q", gotCall, wantCall)
	}
}

// TestExecute_PersistsStateOnPartialFailure verifies T1: if a KDE side
// effect errors mid-apply, the files already copied to disk MUST still
// be tracked in state.toml so `riced revert` can clean them up. Without
// this, a half-finished apply leaves orphans no command knows about.
func TestExecute_PersistsStateOnPartialFailure(t *testing.T) {
	env := setupFakeApply(t)
	env.KDE.ColorSchemeErr = errors.New("plasmashell crashed")

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	execErr := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	})
	if execErr == nil {
		t.Fatal("Execute should have surfaced the injected KDE error")
	}

	// State MUST be on disk even though Execute errored.
	state, err := apply.LoadState(apply.StatePath(env.Home))
	if err != nil {
		t.Fatalf("LoadState after partial failure: %v", err)
	}
	if state == nil {
		t.Fatal("state file missing after partial failure -- revert can't clean up")
	}
	if len(state.WrittenAt) == 0 {
		t.Errorf("state.WrittenAt is empty; expected at least the .colors copy that succeeded before KDE failed")
	}

	// The .colors file was copied before plasma-apply-colorscheme ran,
	// so it must be in WrittenAt.
	wantPath := filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors")
	found := false
	for _, p := range state.WrittenAt {
		if p == wantPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WrittenAt missing %q\ngot: %v", wantPath, state.WrittenAt)
	}
}

// TestExecute_OmitsBackupRootWhenNothingBackedUp verifies R2: a clean
// install (no pre-existing live config files) must not record a
// BackupRoot in state.toml, because the backup dir is never created in
// that case. Recording a phantom path made `riced doctor` flag a
// non-existent dir as an issue.
func TestExecute_OmitsBackupRootWhenNothingBackedUp(t *testing.T) {
	env := setupFakeApply(t)
	// No pre-existing files at any destination -- nothing to back up.

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	state, err := apply.LoadState(apply.StatePath(env.Home))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state == nil {
		t.Fatal("state file missing after Execute")
	}
	if state.BackupRoot != "" {
		t.Errorf("BackupRoot=%q on clean install; want empty (nothing was backed up)", state.BackupRoot)
	}
	if _, err := os.Stat(plan.BackupDir); err == nil {
		t.Errorf("backup dir %s exists on clean install; expected never-created", plan.BackupDir)
	}
}

// TestExecute_RecordsBackupRootWhenSomethingBackedUp is the positive
// counterpart: when at least one file pre-existed and got backed up,
// state.BackupRoot must point at the actual backup dir.
func TestExecute_RecordsBackupRootWhenSomethingBackedUp(t *testing.T) {
	env := setupFakeApply(t)
	// Pre-create one of the destination files so it gets backed up.
	preexist := filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors")
	if err := os.MkdirAll(filepath.Dir(preexist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preexist, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	state, _ := apply.LoadState(apply.StatePath(env.Home))
	if state.BackupRoot == "" {
		t.Error("BackupRoot empty even though one file was backed up")
	}
	if _, err := os.Stat(state.BackupRoot); err != nil {
		t.Errorf("BackupRoot %s does not exist: %v", state.BackupRoot, err)
	}
}

// TestBuild_SkipsBackupForRicedManagedFiles verifies R3: when a previous
// apply already wrote a destination file, Build must mark the new
// Action's PreexistingBackupable=false. Otherwise the second apply would
// back up Riced's own output as if it were the user original, silently
// shadowing the real original captured in the first run.
func TestBuild_SkipsBackupForRicedManagedFiles(t *testing.T) {
	env := setupFakeApply(t)
	// Pre-create the colors destination AND tell Build it's already managed.
	colorsDst := filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors")
	if err := os.MkdirAll(filepath.Dir(colorsDst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(colorsDst, []byte("riced-managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := &apply.State{
		Slug:       "s4-test",
		AppliedAt:  env.Now,
		WrittenAt:  []string{colorsDst},
		BackupRoot: "/some/old/backup",
	}
	plan, err := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), prev)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Find the action that targets colorsDst.
	var got *apply.Action
	for i, a := range plan.Actions {
		if a.Dst == colorsDst {
			got = &plan.Actions[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no action targets %s", colorsDst)
	}
	if got.PreexistingBackupable {
		t.Errorf("PreexistingBackupable=true for Riced-managed file %s; should be false to avoid corrupting the existing backup", colorsDst)
	}
}

// TestBuild_BacksUpUnmanagedExistingFiles is the positive counterpart:
// a destination that exists but is NOT in prev.WrittenAt is a genuine
// user-original and must be flagged for backup.
func TestBuild_BacksUpUnmanagedExistingFiles(t *testing.T) {
	env := setupFakeApply(t)
	colorsDst := filepath.Join(env.Home, ".local/share/color-schemes/s4-test.colors")
	if err := os.MkdirAll(filepath.Dir(colorsDst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(colorsDst, []byte("user-original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// prev has a different file, NOT this one.
	prev := &apply.State{
		Slug:       "other",
		AppliedAt:  env.Now,
		WrittenAt:  []string{filepath.Join(env.Home, ".config/unrelated")},
		BackupRoot: "/some/old/backup",
	}
	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), prev)

	var got *apply.Action
	for i, a := range plan.Actions {
		if a.Dst == colorsDst {
			got = &plan.Actions[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no action targets %s", colorsDst)
	}
	if !got.PreexistingBackupable {
		t.Errorf("PreexistingBackupable=false for unmanaged user file %s; original would be lost", colorsDst)
	}
}

// TestTargets_HonorsXDG verifies T2: when XDG_DATA_HOME / XDG_CONFIG_HOME
// are set, Targets.Map routes files there instead of the
// $HOME/.local/share / $HOME/.config defaults. Without this, Plasma /
// GTK don't find what Riced writes on a non-default XDG layout.
func TestTargets_HonorsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	tg := apply.NewTargets("/ignored/home")

	cases := []struct {
		rel  string
		want string
	}{
		{"slug.colors", "/custom/data/color-schemes/slug.colors"},
		{"konsole/slug.profile", "/custom/data/konsole/slug.profile"},
		{"gtk-3.0/gtk.css", "/custom/config/gtk-3.0/gtk.css"},
		{"gtk-4.0/gtk.css", "/custom/config/gtk-4.0/gtk.css"},
		{"wallpapers/01-foo.png", "/custom/data/riced/wallpapers/slug/01-foo.png"},
		{"icons/launcher.png", "/custom/data/riced/icons/slug-launcher.png"},
	}
	for _, tc := range cases {
		got, ok := tg.Map("slug", tc.rel)
		if !ok {
			t.Errorf("Map(%q) returned ok=false", tc.rel)
			continue
		}
		if got != tc.want {
			t.Errorf("Map(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

// TestTargets_FallsBackToHomeWhenXDGUnset verifies the fallback path:
// no XDG env vars -> standard $HOME/.local/share, $HOME/.config layout.
func TestTargets_FallsBackToHomeWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	tg := apply.NewTargets("/user/home")
	got, _ := tg.Map("slug", "slug.colors")
	want := "/user/home/.local/share/color-schemes/slug.colors"
	if got != want {
		t.Errorf("fallback Map = %q, want %q", got, want)
	}
}

func TestPlan_Format_ListsActions(t *testing.T) {
	env := setupFakeApply(t)
	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
	out := plan.Format()
	for _, want := range []string{
		"s4-test",
		"plasma-apply-colorscheme s4-test",
		// Suffix only: the binary name (qdbus / qdbus6) is resolved at
		// package init from PATH and varies per machine.
		" org.kde.KWin /KWin reconfigure",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan format missing %q\n---\n%s", want, out)
		}
	}
}

// TestApply_F16Customization verifies F16: each new manifest field
// (icons.theme, cursors.theme, plasma.desktop_theme, wallpapers.lock_image)
// produces exactly one kde Action with the expected verb.
func TestApply_F16Customization(t *testing.T) {
	env := setupFakeApply(t)
	env.Manifest.Icons = manifest.Icons{Theme: "breeze-dark"}
	env.Manifest.Cursors = manifest.Cursors{Theme: "capitaine-cursors"}
	env.Manifest.Plasma = manifest.Plasma{DesktopTheme: "default"}

	// Materialize a fake lock wallpaper into the build tree so the planner picks it up.
	lockDir := filepath.Join(env.BuildDir, "lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockSrc := filepath.Join(t.TempDir(), "lock.png")
	if err := os.WriteFile(lockSrc, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(lockSrc, filepath.Join(lockDir, "lock.png")); err != nil {
		t.Fatal(err)
	}

	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)

	wantLock := filepath.Join(env.Home, ".local/share/riced/lock/s4-test-lock.png")
	checks := map[string]bool{
		"lock-wallpaper kwriteconfig6 to kscreenlockerrc": false,
		"icons kwriteconfig6 to kdeglobals":               false,
		"apply-cursor-theme":                              false,
		"apply-desktop-theme":                             false,
	}
	for _, a := range plan.Actions {
		if a.Kind == "kde" && a.Src == "kwriteconfig6" && len(a.Args) == 4 {
			if a.Args[0] == "kscreenlockerrc" && a.Args[3] == wantLock {
				checks["lock-wallpaper kwriteconfig6 to kscreenlockerrc"] = true
			}
			if a.Args[0] == "kdeglobals" && a.Args[1] == "Icons" && a.Args[3] == "breeze-dark" {
				checks["icons kwriteconfig6 to kdeglobals"] = true
			}
		}
		if a.Src == "apply-cursor-theme capitaine-cursors" {
			checks["apply-cursor-theme"] = true
		}
		if a.Src == "apply-desktop-theme default" {
			checks["apply-desktop-theme"] = true
		}
	}
	for k, v := range checks {
		if !v {
			t.Errorf("plan missing %q\n%s", k, plan.Format())
		}
	}

	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: env.KDE,
		Now: func() time.Time { return env.Now },
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantCalls := []string{
		"ApplyCursorTheme:capitaine-cursors",
		"ApplyDesktopTheme:default",
	}
	for _, w := range wantCalls {
		found := false
		for _, c := range env.KDE.Calls {
			if c == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FakeKDE.Calls missing %q\ngot: %v", w, env.KDE.Calls)
		}
	}
}

// TestApply_F17LookAndFeelComesFirst verifies F17: when lookandfeel is
// set, the apply-lookandfeel action appears BEFORE plasma-apply-colorscheme,
// so individual overrides win over the global theme defaults.
func TestApply_F17LookAndFeelComesFirst(t *testing.T) {
	env := setupFakeApply(t)
	env.Manifest.LookAndFeel = manifest.LookAndFeel{Package: "org.kde.breezedark.desktop"}

	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)

	var lafIdx, colorIdx int = -1, -1
	for i, a := range plan.Actions {
		if a.Src == "apply-lookandfeel org.kde.breezedark.desktop" {
			lafIdx = i
		}
		if strings.HasPrefix(a.Src, "plasma-apply-colorscheme ") {
			colorIdx = i
		}
	}
	if lafIdx == -1 {
		t.Fatalf("no apply-lookandfeel action in plan\n%s", plan.Format())
	}
	if colorIdx == -1 {
		t.Fatalf("no plasma-apply-colorscheme action in plan")
	}
	if lafIdx >= colorIdx {
		t.Errorf("apply-lookandfeel must come before plasma-apply-colorscheme; got %d vs %d", lafIdx, colorIdx)
	}
}

// TestApply_F18ExternalPackages verifies F18: external_packages emit
// Kind="external" actions FIRST in the plan and route through the
// Download + InstallExternalPackage path at Execute time.
func TestApply_F18ExternalPackages(t *testing.T) {
	env := setupFakeApply(t)
	env.Manifest.External = []manifest.ExternalPkg{
		{
			Name:   "Test LookAndFeel",
			Type:   "Plasma/LookAndFeel",
			URL:    "https://example.invalid/laf.tar.gz",
			SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	plan, _ := apply.Build(env.Manifest, env.BuildDir,
		apply.Targets{HomeDir: env.Home},
		apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)

	// First "external" action must be at the very top of the actions list
	// among any side-effect, before mkdir/copy/symlink even? Actually plan
	// keeps file ops first, KDE-domain second. external lives in the KDE
	// domain. Verify it comes before any KDE action.
	var extIdx, firstKDEIdx int = -1, -1
	for i, a := range plan.Actions {
		if a.Kind == "external" && extIdx == -1 {
			extIdx = i
		}
		if a.Kind == "kde" && firstKDEIdx == -1 {
			firstKDEIdx = i
		}
	}
	if extIdx == -1 {
		t.Fatalf("no external action in plan\n%s", plan.Format())
	}
	if firstKDEIdx != -1 && extIdx >= firstKDEIdx {
		t.Errorf("external action must precede every kde action; got ext=%d first-kde=%d", extIdx, firstKDEIdx)
	}

	// Execute with an injected download stub that records the call and
	// pretends success. Real DefaultDownload would try to hit example.invalid.
	var downloadedURL, downloadedSHA, downloadedDst string
	fakeDownload := func(url, sha, dst string) error {
		downloadedURL, downloadedSHA, downloadedDst = url, sha, dst
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte("fake-archive"), 0o644)
	}
	if err := apply.Execute(plan, apply.ExecOptions{
		KDE:      env.KDE,
		Now:      func() time.Time { return env.Now },
		Download: fakeDownload,
		HomeDir:  env.Home,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if downloadedURL != "https://example.invalid/laf.tar.gz" {
		t.Errorf("download URL = %q, want example.invalid/laf.tar.gz", downloadedURL)
	}
	if downloadedSHA != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("download SHA passed wrong")
	}
	if !strings.HasPrefix(downloadedDst, filepath.Join(env.Home, ".riced/cache/external")) {
		t.Errorf("download dst %q is not under ~/.riced/cache/external", downloadedDst)
	}
	// FakeKDE must have received InstallExternalPackage.
	wantCall := fmt.Sprintf("InstallExternalPackage:Plasma/LookAndFeel|%s", downloadedDst)
	found := false
	for _, c := range env.KDE.Calls {
		if c == wantCall {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FakeKDE.Calls missing %q\ngot: %v", wantCall, env.KDE.Calls)
	}
}
