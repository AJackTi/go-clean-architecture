// Command bootstrap customizes a clean checkout of this template.
//
// It deliberately operates on tracked text files only.  The command is
// intended to be run immediately after copying the repository as a template,
// before the first project commit.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultRoot = "."
	maxTextFile = 32 << 20 // Do not load an unexpectedly large tracked file.
)

var (
	modulePathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+~-]*(?:/[A-Za-z0-9][A-Za-z0-9.+_~-]*)+$`)
	slugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	ownerPattern      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	emailPattern      = regexp.MustCompile(`[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+`)
	moduleDirective   = regexp.MustCompile(`(?m)^module[\t ]+([^\r\n\t ]+)`)
	copyrightHolder   = regexp.MustCompile(`(?m)^Copyright \(c\)[\t ]+\S+[\t ]+([^\r\n]+?)[\t ]*\r?$`)
)

type options struct {
	root   string
	module string
	slug   string
	owner  string
	author string
	email  string

	dryRun bool
	force  bool
}

type replacement struct {
	old string
	new string
}

type fileChange struct {
	relative string
	path     string
	after    []byte
	mode     fs.FileMode
}

type metadata struct {
	oldModule string
	oldSlug   string
	oldOwner  string
	oldTitle  string
	oldAuthor string
	oldEmails []string
}

// main is intentionally tiny so tests can exercise the command without
// terminating the test process through os.Exit.
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	root, err := gitRoot(opts.root)
	if err != nil {
		return err
	}
	if !opts.force {
		if err := requireCleanWorktree(root); err != nil {
			return err
		}
	}

	files, err := trackedFiles(root)
	if err != nil {
		return err
	}
	meta, err := readMetadata(root, files)
	if err != nil {
		return err
	}
	replacements, err := makeReplacements(meta, opts)
	if err != nil {
		return err
	}

	changes, skipped, err := collectChanges(root, files, replacements)
	if err != nil {
		return err
	}
	if opts.dryRun {
		for _, change := range changes {
			fmt.Fprintf(stdout, "would update %s\n", change.relative)
		}
		fmt.Fprintf(stdout, "dry-run: %d file(s) would change", len(changes))
		if skipped > 0 {
			fmt.Fprintf(stdout, "; %d binary/symlink file(s) skipped", skipped)
		}
		fmt.Fprintln(stdout)
		return nil
	}

	for _, change := range changes {
		if err := atomicReplace(change.path, change.after, change.mode); err != nil {
			return fmt.Errorf("rewrite %s: %w", change.relative, err)
		}
		fmt.Fprintf(stdout, "updated %s\n", change.relative)
	}
	fmt.Fprintf(stdout, "bootstrap: %d file(s) updated", len(changes))
	if skipped > 0 {
		fmt.Fprintf(stdout, "; %d binary/symlink file(s) skipped", skipped)
	}
	fmt.Fprintln(stdout)
	return nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{root: defaultRoot}
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: bootstrap --module MODULE --slug SLUG [options]")
		fmt.Fprintln(stderr, "")
		flags.PrintDefaults()
	}
	flags.StringVar(&opts.module, "module", "", "new Go module path (required)")
	flags.StringVar(&opts.module, "module-path", "", "alias for --module")
	flags.StringVar(&opts.module, "m", "", "alias for --module")
	flags.StringVar(&opts.slug, "slug", "", "new lower-case project/repository slug (required)")
	flags.StringVar(&opts.slug, "project-slug", "", "alias for --slug")
	flags.StringVar(&opts.slug, "s", "", "alias for --slug")
	flags.StringVar(&opts.owner, "owner", "", "GitHub owner/login to use for maintainer references")
	flags.StringVar(&opts.owner, "github-owner", "", "alias for --owner")
	flags.StringVar(&opts.owner, "o", "", "alias for --owner")
	flags.StringVar(&opts.author, "author", "", "display name for plain-text maintainer references")
	flags.StringVar(&opts.email, "email", "", "maintainer email address")
	flags.StringVar(&opts.email, "author-email", "", "alias for --email")
	flags.StringVar(&opts.root, "root", defaultRoot, "template checkout or a directory inside it")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "show changes without writing files")
	flags.BoolVar(&opts.force, "force", false, "allow running with a dirty worktree")
	flags.BoolVar(&opts.force, "allow-dirty", false, "alias for --force")

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	positional := flags.Args()
	if opts.module == "" && len(positional) > 0 {
		opts.module = positional[0]
		positional = positional[1:]
	}
	if opts.slug == "" && len(positional) > 0 {
		opts.slug = positional[0]
		positional = positional[1:]
	}
	if len(positional) > 0 {
		return options{}, fmt.Errorf("unexpected positional argument(s): %s", strings.Join(positional, " "))
	}

	opts.module = strings.TrimSpace(opts.module)
	opts.slug = strings.TrimSpace(opts.slug)
	opts.owner = strings.TrimSpace(opts.owner)
	opts.author = strings.TrimSpace(opts.author)
	opts.email = strings.TrimSpace(opts.email)
	opts.root = strings.TrimSpace(opts.root)
	if opts.root == "" {
		opts.root = defaultRoot
	}
	if err := validateOptions(opts); err != nil {
		return options{}, err
	}
	return opts, nil
}

func validateOptions(opts options) error {
	if opts.module == "" {
		return errors.New("--module is required")
	}
	if opts.slug == "" {
		return errors.New("--slug is required")
	}
	if !modulePathPattern.MatchString(opts.module) || strings.Contains(opts.module, "//") {
		return fmt.Errorf("invalid module path %q", opts.module)
	}
	for _, segment := range strings.Split(opts.module, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid module path %q", opts.module)
		}
	}
	if !slugPattern.MatchString(opts.slug) {
		return fmt.Errorf("invalid project slug %q (use lower-case letters, digits, '_' or '-')", opts.slug)
	}
	if opts.owner != "" && !ownerPattern.MatchString(opts.owner) {
		return fmt.Errorf("invalid GitHub owner %q", opts.owner)
	}
	if opts.author != "" && (!utf8.ValidString(opts.author) || strings.IndexFunc(opts.author, unicode.IsControl) >= 0) {
		return fmt.Errorf("invalid author %q (control characters are not allowed)", opts.author)
	}
	if opts.email != "" {
		address, err := mail.ParseAddress(opts.email)
		if err != nil || address.Address != opts.email || !emailPattern.MatchString(opts.email) {
			return fmt.Errorf("invalid email address %q", opts.email)
		}
	}
	return nil
}

func gitRoot(input string) (string, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root %q is not a directory", input)
	}

	out, err := runGit(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%q is not inside a Git worktree: %w", input, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("git returned an empty worktree root")
	}
	return filepath.Clean(root), nil
}

func requireCleanWorktree(root string) error {
	out, err := runGit(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect Git worktree: %w", err)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return errors.New("git worktree is dirty; commit/stash changes or rerun with --force")
	}
	return nil
}

func trackedFiles(root string) ([]string, error) {
	out, err := runGit(root, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}
	raw := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(raw))
	for _, entry := range raw {
		if len(entry) == 0 {
			continue
		}
		files = append(files, string(entry))
	}
	sort.Strings(files)
	return files, nil
}

func runGit(root string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", root}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return output, nil
}

func readMetadata(root string, files []string) (metadata, error) {
	goModPath := filepath.Join(root, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return metadata{}, fmt.Errorf("read go.mod: %w", err)
	}
	match := moduleDirective.FindSubmatch(goMod)
	if len(match) != 2 {
		return metadata{}, errors.New("go.mod does not contain a module directive")
	}
	oldModule := string(match[1])
	oldSlug := projectSlug(oldModule)
	oldOwner := githubOwner(oldModule)
	emails, err := discoverEmails(root, files)
	if err != nil {
		return metadata{}, err
	}
	author, err := discoverCopyrightHolder(root, files)
	if err != nil {
		return metadata{}, err
	}
	return metadata{
		oldModule: oldModule,
		oldSlug:   oldSlug,
		oldOwner:  oldOwner,
		oldTitle:  titleFromSlug(oldSlug),
		oldAuthor: author,
		oldEmails: emails,
	}, nil
}

func discoverCopyrightHolder(root string, files []string) (string, error) {
	for _, relative := range files {
		if filepath.Clean(filepath.FromSlash(relative)) != "LICENSE" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "LICENSE"))
		if err != nil {
			return "", fmt.Errorf("read LICENSE: %w", err)
		}
		match := copyrightHolder.FindSubmatch(data)
		if len(match) != 2 {
			return "", nil
		}
		return strings.TrimSpace(string(match[1])), nil
	}
	return "", nil
}

func discoverEmails(root string, files []string) ([]string, error) {
	// Restrict discovery to policy documents.  Contribution guides commonly
	// contain database URLs such as user:password@127.0.0.1, which are not
	// maintainer addresses and must not make --email ambiguous.
	knownDocs := map[string]bool{
		"SECURITY.md":        true,
		"CODE_OF_CONDUCT.md": true,
	}
	seen := make(map[string]struct{})
	for _, relative := range files {
		if !knownDocs[filepath.Base(relative)] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relative, err)
		}
		for _, found := range emailPattern.FindAllString(string(data), -1) {
			seen[found] = struct{}{}
		}
	}
	emails := make([]string, 0, len(seen))
	for found := range seen {
		emails = append(emails, found)
	}
	sort.Strings(emails)
	return emails, nil
}

func makeReplacements(meta metadata, opts options) ([]replacement, error) {
	newOwner := opts.owner
	if newOwner == "" {
		newOwner = githubOwner(opts.module)
	}
	if newOwner != "" && !ownerPattern.MatchString(newOwner) {
		return nil, fmt.Errorf("invalid GitHub owner %q", newOwner)
	}
	if meta.oldOwner != "" && newOwner == "" {
		return nil, errors.New("the new module is not a GitHub path; pass --owner to replace the existing GitHub owner")
	}
	plainOwner := newOwner
	if opts.author != "" {
		plainOwner = opts.author
	}

	items := []replacement{
		{old: meta.oldModule, new: opts.module},
		{old: meta.oldSlug, new: opts.slug},
		{old: meta.oldTitle, new: titleFromSlug(opts.slug)},
	}
	if meta.oldOwner != "" && newOwner != "" {
		// More specific forms protect URLs and CODEOWNERS handles when the
		// optional display author differs from the GitHub login.
		items = append(items,
			replacement{old: "github.com/" + meta.oldOwner, new: "github.com/" + newOwner},
			replacement{old: "@" + meta.oldOwner, new: "@" + newOwner},
			replacement{old: meta.oldOwner, new: plainOwner},
		)
	}
	if meta.oldAuthor != "" && plainOwner != "" {
		items = append(items, replacement{old: meta.oldAuthor, new: plainOwner})
	}
	if len(meta.oldEmails) > 0 && opts.email == "" {
		return nil, errors.New("--email is required to replace the maintainer address found in project policy files")
	}
	if opts.email != "" {
		if len(meta.oldEmails) == 0 {
			return nil, errors.New("--email was supplied but no maintainer email was found in project policy files")
		}
		if len(meta.oldEmails) > 1 {
			return nil, fmt.Errorf("--email is ambiguous; found %d maintainer addresses", len(meta.oldEmails))
		}
		items = append(items, replacement{old: meta.oldEmails[0], new: opts.email})
	}
	return normalizeReplacements(items), nil
}

func normalizeReplacements(items []replacement) []replacement {
	seen := make(map[string]struct{}, len(items))
	result := make([]replacement, 0, len(items))
	for _, item := range items {
		if item.old == "" || item.old == item.new {
			continue
		}
		if _, exists := seen[item.old]; exists {
			continue
		}
		seen[item.old] = struct{}{}
		result = append(result, item)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if len(result[left].old) == len(result[right].old) {
			return result[left].old < result[right].old
		}
		return len(result[left].old) > len(result[right].old)
	})
	return result
}

func collectChanges(root string, files []string, replacements []replacement) ([]fileChange, int, error) {
	changes := make([]fileChange, 0)
	skipped := 0
	for _, relative := range files {
		absolute, err := safePath(root, relative)
		if err != nil {
			return nil, skipped, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, skipped, fmt.Errorf("stat %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			skipped++
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > maxTextFile {
			skipped++
			continue
		}
		before, err := os.ReadFile(absolute)
		if err != nil {
			return nil, skipped, fmt.Errorf("read %s: %w", relative, err)
		}
		if !isText(before) {
			skipped++
			continue
		}
		after := replaceOnce(before, replacements)
		if bytes.Equal(before, after) {
			continue
		}
		changes = append(changes, fileChange{
			relative: filepath.ToSlash(relative),
			path:     absolute,
			after:    after,
			mode:     info.Mode(),
		})
	}
	return changes, skipped, nil
}

func safePath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe tracked path %q", relative)
	}
	joined := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("tracked path escapes worktree: %q", relative)
	}
	return joined, nil
}

func isText(data []byte) bool {
	return !bytes.Contains(data, []byte{0}) && utf8.Valid(data)
}

// replaceOnce applies every replacement while scanning the original bytes.
// Newly written values are never scanned again, so overlapping values cannot
// cascade (for example, a new slug containing the old slug).
func replaceOnce(input []byte, replacements []replacement) []byte {
	if len(replacements) == 0 {
		return append([]byte(nil), input...)
	}
	var output bytes.Buffer
	output.Grow(len(input))
	source := string(input)
	for offset := 0; offset < len(input); {
		matched := false
		for _, item := range replacements {
			if strings.HasPrefix(source[offset:], item.old) {
				output.WriteString(item.new)
				offset += len(item.old)
				matched = true
				break
			}
		}
		if !matched {
			output.WriteByte(input[offset])
			offset++
		}
	}
	return output.Bytes()
}

func atomicReplace(filename string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(filename)
	temporary, err := os.CreateTemp(directory, ".bootstrap-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	permission := mode.Perm()
	permission |= mode & (fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	if err := temporary.Chmod(permission); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return err
	}
	removeTemporary = false
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	// Directory fsync is not supported on every platform (notably Windows).
	// The file itself is synced before rename; an unsupported directory sync
	// should not turn an otherwise atomic replacement into a failure.
	syncErr := directoryHandle.Sync()
	closeErr := directoryHandle.Close()
	if syncErr != nil && !errors.Is(syncErr, os.ErrInvalid) {
		return syncErr
	}
	return closeErr
}

func projectSlug(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) > 1 && last[0] == 'v' {
		allDigits := true
		for _, character := range last[1:] {
			if character < '0' || character > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(parts) > 1 {
			return parts[len(parts)-2]
		}
	}
	return last
}

func githubOwner(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) >= 3 && strings.EqualFold(parts[0], "github.com") {
		return parts[1]
	}
	return ""
}

func titleFromSlug(slug string) string {
	parts := strings.FieldsFunc(slug, func(character rune) bool {
		return character == '-' || character == '_' || character == '.'
	})
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
