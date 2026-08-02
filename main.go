package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"mailtui/internal/config"
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
	if flags.NArg() > 1 {
		return errors.New("usage: mailtui [MAILDIR_ROOT]")
	}

	root, err := resolveRoot(flags.Args())
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

func resolveRoot(args []string) (string, error) {
	var root string
	if len(args) == 1 {
		root = args[0]
	} else {
		configPath, err := config.FilePath()
		if err != nil {
			return "", err
		}
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("no Maildir root provided; pass one as an argument or create %s", configPath)
			}
			return "", fmt.Errorf("load configuration %s: %w", configPath, err)
		}
		root, err = config.ExpandHome(cfg.Maildir)
		if err != nil {
			return "", err
		}
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return root, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mailtui:", err)
		os.Exit(1)
	}
}
