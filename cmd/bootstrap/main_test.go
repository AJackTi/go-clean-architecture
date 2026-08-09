package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixtureModule = "github.com/oldowner/old-slug"
	fixtureEmail  = "old@example.com"
)

func TestRunRewritesTrackedTextAndPreservesModes(t *testing.T) {
	root := newFixture(t)
	executable := filepath.Join(root, "scripts", "run.sh")
	beforeMode := fileMode(t, executable)
	binaryBefore := readFile(t, filepath.Join(root, "assets", "fixture.bin"))
	linkBefore := readFile(t, filepath.Join(root, "assets", "link.txt"))

	var output bytes.Buffer
	err := run([]string{
		"--root", root,
		"--module", "github.com/newowner/new-slug-next",
		"--slug", "new-slug-next",
		"--owner", "newowner",
		"--author", "New Author",
		"--email", "new@example.com",
	}, &output, &output)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	goMod := string(readFile(t, filepath.Join(root, "go.mod")))
	if !strings.Contains(goMod, "module github.com/newowner/new-slug-next") {
		t.Fatalf("go.mod was not updated: %s", goMod)
	}
	readme := string(readFile(t, filepath.Join(root, "README.md")))
	for _, want := range []string{
		"github.com/newowner/new-slug-next",
		"@newowner",
		"new-slug-next",
		"New Slug Next",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README missing replacement %q: %s", want, readme)
		}
	}
	if strings.Contains(readme, "old-slug") || strings.Contains(readme, "oldowner") {
		t.Errorf("README still contains old project tokens: %s", readme)
	}

	license := string(readFile(t, filepath.Join(root, "LICENSE")))
	if !strings.Contains(license, "Copyright (c) 2024-present New Author") {
		t.Fatalf("copyright holder was not updated: %s", license)
	}
	security := string(readFile(t, filepath.Join(root, "SECURITY.md")))
	if !strings.Contains(security, "new@example.com") || strings.Contains(security, fixtureEmail) {
		t.Fatalf("maintainer email was not updated: %s", security)
	}

	if got := fileMode(t, executable); got != beforeMode {
		t.Fatalf("executable mode changed: before=%#o after=%#o", beforeMode, got)
	}
	if got := readFile(t, filepath.Join(root, "assets", "fixture.bin")); !bytes.Equal(got, binaryBefore) {
		t.Fatal("binary tracked file should not be rewritten")
	}
	if got := readFile(t, filepath.Join(root, "assets", "link.txt")); !bytes.Equal(got, linkBefore) {
		t.Fatal("external symlink target should not be rewritten")
	}
	for _, want := range []string{"updated go.mod", "updated README.md", "bootstrap: ", "binary/symlink"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output.String())
		}
	}
}

func TestRunDryRunDoesNotWrite(t *testing.T) {
	root := newFixture(t)
	paths := []string{"go.mod", "README.md", "LICENSE", "SECURITY.md", "scripts/run.sh"}
	before := make(map[string][]byte, len(paths))
	for _, relative := range paths {
		before[relative] = readFile(t, filepath.Join(root, relative))
	}

	var output bytes.Buffer
	err := run([]string{
		"--root", root,
		"--module", "example.com/acme/new-project",
		"--slug", "new-project",
		"--owner", "acme",
		"--author", "Acme Team",
		"--email", "new@example.com",
		"--dry-run",
	}, &output, &output)
	if err != nil {
		t.Fatalf("dry-run error = %v", err)
	}
	if !strings.Contains(output.String(), "would update") || strings.Contains(output.String(), "updated ") {
		t.Fatalf("unexpected dry-run output: %s", output.String())
	}
	for _, relative := range paths {
		if got := readFile(t, filepath.Join(root, relative)); !bytes.Equal(got, before[relative]) {
			t.Errorf("dry-run changed %s", relative)
		}
	}
}

func TestRunIsIdempotentAfterOwnerAndAuthorCustomization(t *testing.T) {
	root := newFixture(t)
	args := []string{
		"--root", root,
		"--module", "github.com/newowner/new-project",
		"--slug", "new-project",
		"--owner", "newowner",
		"--author", "New Author",
		"--email", "new@example.com",
	}

	var output bytes.Buffer
	if err := run(args, &output, &output); err != nil {
		t.Fatalf("first bootstrap error = %v", err)
	}

	output.Reset()
	secondArgs := append(append([]string(nil), args...), "--force", "--dry-run")
	if err := run(secondArgs, &output, &output); err != nil {
		t.Fatalf("second bootstrap dry-run error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "dry-run: 0 file(s) would change") {
		t.Fatalf("second bootstrap would rewrite files: %s", got)
	}
}

func TestMetadataSkipsEscapingPolicySymlinks(t *testing.T) {
	root := newFixture(t)
	external := t.TempDir()
	externalLicense := filepath.Join(external, "LICENSE")
	externalSecurity := filepath.Join(external, "SECURITY.md")
	if err := os.WriteFile(externalLicense, []byte("Copyright (c) 2099 External Maintainer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalSecurity, []byte("Contact external@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for relative, target := range map[string]string{
		"LICENSE":     externalLicense,
		"SECURITY.md": externalSecurity,
	} {
		path := filepath.Join(root, relative)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}

	files, err := trackedFiles(root)
	if err != nil {
		t.Fatalf("trackedFiles() error = %v", err)
	}
	meta, err := readMetadata(root, files)
	if err != nil {
		t.Fatalf("readMetadata() error = %v", err)
	}
	if meta.oldAuthor != "" {
		t.Fatalf("metadata read copyright holder through symlink: %q", meta.oldAuthor)
	}
	if len(meta.oldEmails) != 0 {
		t.Fatalf("metadata read maintainer emails through symlink: %v", meta.oldEmails)
	}

	var output bytes.Buffer
	if err := run([]string{
		"--root", root,
		"--module", "github.com/newowner/new-project",
		"--slug", "new-project",
		"--owner", "newowner",
		"--author", "New Author",
		"--force",
	}, &output, &output); err != nil {
		t.Fatalf("bootstrap with policy symlinks error = %v", err)
	}
	for relative, target := range map[string]string{
		"LICENSE":     externalLicense,
		"SECURITY.md": externalSecurity,
	} {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s was rewritten instead of remaining a symlink", relative)
		}
		resolved, err := filepath.EvalSymlinks(path)
		wantTarget, targetErr := filepath.EvalSymlinks(target)
		if err != nil || targetErr != nil || resolved != wantTarget {
			t.Errorf("%s target = %q, want %q (err=%v, target err=%v)", relative, resolved, wantTarget, err, targetErr)
		}
	}
	if got := string(readFile(t, externalLicense)); got != "Copyright (c) 2099 External Maintainer\n" {
		t.Errorf("external LICENSE was modified: %q", got)
	}
	if got := string(readFile(t, externalSecurity)); got != "Contact external@example.com\n" {
		t.Errorf("external SECURITY.md was modified: %q", got)
	}
}

func TestRunRefusesDirtyWorktreeUnlessForced(t *testing.T) {
	root := newFixture(t)
	readmePath := filepath.Join(root, "README.md")
	if err := os.WriteFile(readmePath, []byte("dirty old-slug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readmePath, 0o644); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	err := run([]string{
		"--root", root,
		"--module", "example.com/acme/new-project",
		"--slug", "new-project",
	}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty worktree error = %v, want dirty-worktree refusal", err)
	}
	if got := string(readFile(t, readmePath)); got != "dirty old-slug\n" {
		t.Fatalf("refused run changed dirty file: %q", got)
	}

	err = run([]string{
		"--root", root,
		"--module", "example.com/acme/new-project",
		"--slug", "new-project",
		"--owner", "acme",
		"--email", "new@example.com",
		"--force",
	}, &output, &output)
	if err != nil {
		t.Fatalf("forced run error = %v", err)
	}
	if got := string(readFile(t, readmePath)); !strings.Contains(got, "new-project") {
		t.Fatalf("forced run did not rewrite dirty file: %q", got)
	}
}

func TestRunRejectsLeavingTemplateIdentity(t *testing.T) {
	root := newFixture(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "maintainer email",
			args: []string{
				"--root", root,
				"--module", "github.com/newowner/new-project",
				"--slug", "new-project",
			},
			want: "--email is required",
		},
		{
			name: "GitHub owner for non-GitHub module",
			args: []string{
				"--root", root,
				"--module", "example.com/acme/new-project",
				"--slug", "new-project",
				"--email", "new@example.com",
			},
			want: "pass --owner",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(test.args, &output, &output)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestReplaceOnceDoesNotCascade(t *testing.T) {
	replacements := []replacement{
		{old: "old-slug", new: "new-old-slug"},
		{old: "old", new: "replacement"},
	}
	got := string(replaceOnce([]byte("old-slug old"), normalizeReplacements(replacements)))
	if want := "new-old-slug replacement"; got != want {
		t.Fatalf("replaceOnce() = %q, want %q", got, want)
	}
}

func TestRunRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing module", args: []string{"--slug", "project"}, want: "--module is required"},
		{name: "missing slug", args: []string{"--module", "example.com/acme/project"}, want: "--slug is required"},
		{name: "bad module", args: []string{"--module", "../escape", "--slug", "project"}, want: "invalid module path"},
		{name: "bad slug", args: []string{"--module", "example.com/acme/project", "--slug", "Project"}, want: "invalid project slug"},
		{name: "compose-unsafe slug", args: []string{"--module", "example.com/acme/project", "--slug", "project.name"}, want: "invalid project slug"},
		{name: "bad owner", args: []string{"--module", "example.com/acme/project", "--slug", "project", "--owner", "bad/owner"}, want: "invalid GitHub owner"},
		{name: "bad author", args: []string{"--module", "example.com/acme/project", "--slug", "project", "--author", "bad\nname"}, want: "invalid author"},
		{name: "bad email", args: []string{"--module", "example.com/acme/project", "--slug", "project", "--email", "not-an-email"}, want: "invalid email"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(test.args, &output, &output)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{
		"go.mod":             []byte("module " + fixtureModule + "\n\ngo 1.26.0\n"),
		"README.md":          []byte("# Old Slug\nmodule: " + fixtureModule + "\nrepo: https://github.com/oldowner/old-slug\nhandle: @oldowner\nslug: old-slug\nfuture: new-old-slug\n"),
		"LICENSE":            []byte("MIT License\n\nCopyright (c) 2024-present Old Author\n"),
		"SECURITY.md":        []byte("Contact old@example.com for security reports.\n"),
		"CONTRIBUTING.md":    []byte("DATABASE_URL=postgres://app:local-dev-password@127.0.0.1:5432/app\n"),
		"scripts/run.sh":     []byte("#!/bin/sh\necho old-slug\n"),
		"assets/fixture.bin": {0x00, 'o', 'l', 'd', '-', 's', 'l', 'u', 'g'},
	}
	for relative, data := range files {
		filename := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filename, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "scripts/run.sh"), 0o750); err != nil {
		t.Fatal(err)
	}
	linkTarget := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(linkTarget, []byte("old-slug external target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, filepath.Join(root, "assets/link.txt")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "config", "user.name", "Bootstrap Test")
	gitTest(t, root, "config", "user.email", "bootstrap@example.com")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-q", "-m", "fixture")
	return root
}

func gitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func readFile(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fileMode(t *testing.T, filename string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
