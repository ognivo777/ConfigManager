// Package cli implements the cm command-line interface. It communicates with
// the daemon over the local API and never manipulates Git or monitored files
// directly.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/cm/internal/api"
	"github.com/example/cm/internal/config"
	"github.com/example/cm/internal/daemon"
)

// Run parses arguments and executes the corresponding command. Returns a
// process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "-d":
		repo := ""
		if len(args) > 1 {
			repo = args[1]
		}
		if repo == "" {
			repo = config.DefaultRepository
		}
		if err := runDaemon(repo); err != nil {
			fmt.Fprintln(os.Stderr, "cm: daemon error:", err)
			return 1
		}
		return 0

	case "add":
		if err := cmdAdd(args[1:]); err != nil {
			return fail(err)
		}
	case "history":
		if err := cmdHistory(args[1:]); err != nil {
			return fail(err)
		}
	case "diff":
		if err := cmdDiff(args[1:]); err != nil {
			return fail(err)
		}
	case "ls":
		if err := cmdList(args[1:]); err != nil {
			return fail(err)
		}
	case "status":
		if err := cmdStatus(args[1:]); err != nil {
			return fail(err)
		}
	case "message":
		if err := cmdMessage(args[1:]); err != nil {
			return fail(err)
		}
	case "restore":
		if err := cmdRestore(args[1:]); err != nil {
			return fail(err)
		}
	case "start":
		return serviceControl("start")
	case "stop":
		return serviceControl("stop")
	case "--install":
		if err := cmdInstall(args[1:]); err != nil {
			return fail(err)
		}
	case "--uninstall":
		if err := cmdUninstall(); err != nil {
			return fail(err)
		}
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "cm: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
	return 0
}

// fail prints an error to stderr and returns exit code 1.
func fail(err error) int {
	fmt.Fprintln(os.Stderr, "cm:", err)
	return 1
}

func newClient() *api.Client {
	return api.NewClient(config.SocketPath(config.DefaultRepository))
}

func doCall(method string, params any, out any) error {
	return newClient().Call(method, params, out)
}

func cmdAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cm add <path> [<path> ...]")
	}

	// Validate every path up front so one bad argument does not partially
	// register the rest.
	abs := make([]string, 0, len(args))
	for _, p := range args {
		a, err := resolveValidate(p)
		if err != nil {
			return err
		}
		abs = append(abs, a)
	}

	var errs []error
	for _, a := range abs {
		if err := doCall(api.MAdd, daemon.AddRequest{AbsPath: a}, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a, err))
		} else {
			fmt.Println("Add monitored file:", a)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// cmdList reports which monitored files live under the current working
// directory, including deleted ones. With --all it lists every monitored file.
func cmdList(args []string) error {
	all := false
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "-a":
			all = true
		default:
			return fmt.Errorf("unknown option: %s", a)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cwd = filepath.Clean(cwd)

	var entries []daemon.ListEntry
	if err := doCall(api.MList, nil, &entries); err != nil {
		return err
	}

	type row struct{ path, state string }
	var rows []row
	for _, e := range entries {
		abs := filepath.Clean(e.AbsPath)
		var display string
		if all {
			display = abs
		} else {
			// match files in the current directory or any subdirectory
			if !isUnder(abs, cwd) {
				continue
			}
			display, _ = filepath.Rel(cwd, abs)
		}
		state := strings.ToUpper(e.State)
		if !e.Exists {
			state = "DELETED"
		}
		rows = append(rows, row{path: display, state: state})
	}

	if len(rows) == 0 {
		if all {
			fmt.Println("no monitored files")
		} else {
			fmt.Println("no monitored files under " + cwd)
		}
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-40s %s\n", r.path, r.state)
	}
	return nil
}

// cmdStatus lists every entry in the current directory and marks which ones
// are monitored by cm.
func cmdStatus(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: cm status")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cwd = filepath.Clean(cwd)

	var entries []daemon.ListEntry
	if err := doCall(api.MList, nil, &entries); err != nil {
		return err
	}
	// set of absolute monitored paths
	monitored := make(map[string]bool, len(entries))
	for _, e := range entries {
		monitored[filepath.Clean(e.AbsPath)] = true
	}

	dirEntries, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}
	for _, de := range dirEntries {
		name := de.Name()
		abs := filepath.Join(cwd, name)
		marker := " "
		if monitored[filepath.Clean(abs)] {
			marker = "*"
		}
		kind := " "
		if de.IsDir() {
			kind = "/"
			name = name + "/"
		}
		fmt.Printf("%s %-40s %s\n", marker, name, kind)
	}
	return nil
}

// isUnder reports whether p is cwd itself or underneath it.
func isUnder(p, dir string) bool {
	if p == dir {
		return true
	}
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveValidate resolves a CLI argument to an absolute, normalized path and
// validates it exists and is a regular file.
func resolveValidate(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", p)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("not a regular file: %s", p)
	}
	return abs, nil
}

func cmdHistory(args []string) error {
	var all bool
	var file string
	for _, a := range args {
		switch {
		case a == "--all":
			all = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown option: %s", a)
		case file == "":
			file = a
		default:
			return fmt.Errorf("too many arguments")
		}
	}

	opts := daemon.HistoryOptions{Limit: 20, File: file}
	if all {
		opts.Limit = 0
	}
	if file != "" {
		resolved, err := resolvePath(file)
		if err != nil {
			return err
		}
		opts.File = resolved
	}

	var entries []daemon.HistoryEntry
	if err := doCall(api.MHistory, opts, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Printf("%s\t%s\t%s\t%s\n", shortDate(e.Date), e.File, e.Change, shortID(e.Commit))
		if e.Subject != "" {
			for _, l := range strings.Split(e.Subject, "\n") {
				fmt.Printf("  %s\n", l)
			}
		}
		fmt.Println()
	}
	return nil
}

func cmdDiff(args []string) error {
	opts := daemon.DiffOptions{}
	var commit string
	var file string
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("-c requires a commit id")
			}
			i++
			commit = args[i]
		case strings.HasPrefix(a, "-c="):
			commit = strings.TrimPrefix(a, "-c=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown option: %s", a)
		case file == "":
			file = a
		default:
			return fmt.Errorf("too many arguments")
		}
		i++
	}
	opts.Commit = commit
	if file != "" {
		resolved, err := resolvePath(file)
		if err != nil {
			return err
		}
		opts.File = resolved
	}

	var diff string
	if err := doCall(api.MDiff, opts, &diff); err != nil {
		return err
	}
	fmt.Print(diff)
	return nil
}

func cmdMessage(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: cm message (reads text from stdin until EOF)")
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(b), "\n")
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("no message provided")
	}
	return doCall(api.MMessage, text, nil)
}

func cmdRestore(args []string) error {
	var commit string
	var all bool
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown option: %s", a)
			}
			if commit != "" {
				return fmt.Errorf("too many arguments")
			}
			commit = a
		}
	}
	if commit == "" {
		return fmt.Errorf("usage: cm restore <commit_id> [--all]")
	}

	var prev daemon.RestorePreview
	if err := doCall(api.MPreview, commit, &prev); err != nil {
		return err
	}
	if len(prev.Files) == 0 {
		return fmt.Errorf("commit %s affects no registered monitored files", commit)
	}

	confirmed, err := confirmRestore(prev)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("Restore cancelled.")
		return nil
	}

	var result daemon.RestoreResult
	if err := doCall(api.MRestore, daemon.RestoreRequest{Commit: commit, All: all}, &result); err != nil {
		return err
	}
	fmt.Println("Restored files:")
	for _, f := range result.Restored {
		fmt.Println("  ", f)
	}
	fmt.Printf("Restore commit: %s\n", result.Commit)
	return nil
}

func confirmRestore(prev daemon.RestorePreview) (bool, error) {
	fmt.Printf("Restore from commit %s?\n\n", prev.Commit)
	for _, f := range prev.Files {
		fmt.Printf("File: %s\n", f.AbsPath)
	}
	fmt.Printf("\nCommit message:\n%s\n\n", prev.Message)
	fmt.Print("Options:\n  [d] Show diff against current version\n  [y] Restore\n  [n] Cancel\n> ")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		switch strings.TrimSpace(line) {
		case "y", "Y":
			return true, nil
		case "n", "N":
			return false, nil
		case "d", "D":
			var diff string
			if err := doCall(api.MDiff, daemon.DiffOptions{Commit: prev.Commit}, &diff); err != nil {
				return false, err
			}
			fmt.Println(diff)
			fmt.Print("\n[y] Restore  [n] Cancel\n> ")
		default:
			fmt.Print("Invalid choice. [y] Restore  [n] Cancel\n> ")
		}
	}
}

func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func shortDate(iso string) string {
	iso = strings.ReplaceAll(iso, "T", " ")
	if i := strings.Index(iso, "+"); i > 0 {
		iso = iso[:i]
	}
	return iso
}

func shortID(full string) string {
	if len(full) > 7 {
		return full[:7]
	}
	return full
}

func usage(w io.Writer) {
	fmt.Fprint(w, `cm — Config Manager

Usage:
  cm                                   Show this help
  cm -d [/path/to/repository]          Run the daemon
  cm add <path> [<path> ...]             Register one or more monitored files
  cm ls [--all]                        List monitored files under the current directory (--all for complete list)
  cm status                           List all entries in the current directory, marking monitored ones (*)
  cm history [<file>] [--all]          Show snapshot history
  cm diff [<file>] [-c <commit>]       Show a diff
  cm message                           Set the message for the next commit (from stdin)
  cm restore <commit_id> [--all]       Restore files from a historical commit
  cm start                             Start the installed service
  cm stop                              Stop the installed service
  cm --install [path]                  Install the service
  cm --uninstall                       Uninstall the service
`)
}
