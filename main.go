package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"mailtui/internal/maildir"
	"mailtui/internal/ui"
)

func run(args []string) error {
	flags := flag.NewFlagSet("mailtui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: mailtui MAILDIR_ROOT")
	}

	root, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return err
	}
	folders, err := maildir.Discover(root)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("no Maildir folders found under %s", root)
	}

	_, err = tea.NewProgram(ui.New(root, folders), tea.WithAltScreen()).Run()
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mailtui:", err)
		os.Exit(1)
	}
}
