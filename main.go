package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"mailtui/internal/maildir"
	"mailtui/internal/ui"
)

var version = "dev"

func run(args []string) error {
	flags := flag.NewFlagSet("mailtui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(os.Stdout, "mailtui "+version)
		return nil
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

	_, err = tea.NewProgram(ui.New(root, folders)).Run()
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mailtui:", err)
		os.Exit(1)
	}
}
