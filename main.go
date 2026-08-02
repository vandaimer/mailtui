package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"mailtui/internal/config"
	"mailtui/internal/maildir"
	"mailtui/internal/ui"
	"mailtui/internal/updater"
)

var version = "dev"
var errConfigurationMissing = errors.New("mailtui configuration is missing")

func run(args []string) error {
	return runWithIO(args, os.Stdout, os.Stderr)
}

func runWithIO(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 {
		switch args[0] {
		case "help", "-h", "--help":
			return writeHelp(stdout)
		case "version", "--version":
			fmt.Fprintln(stdout, "mailtui "+version)
			return nil
		case "config":
			return showConfiguration(stdout)
		case "update":
			return updater.Update(context.Background(), version, stdout)
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "help", "version", "config", "update":
			return fmt.Errorf("%s does not accept additional arguments", args[0])
		}
	}

	flags := flag.NewFlagSet("mailtui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version and exit")
	configPath := flags.String("config", "", "read configuration from this file")
	flags.Usage = func() { _ = writeHelp(stderr) }
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "mailtui "+version)
		return nil
	}
	if flags.NArg() > 1 {
		return errors.New("only one Maildir root may be provided; run 'mailtui help' for usage")
	}
	if *configPath != "" && flags.NArg() != 0 {
		return errors.New("--config and MAILDIR_ROOT cannot be used together")
	}

	root, err := resolveRootWithConfig(flags.Args(), *configPath)
	if err != nil {
		if errors.Is(err, errConfigurationMissing) && *configPath == "" && flags.NArg() == 0 {
			return writeWelcome(stdout)
		}
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
	return resolveRootWithConfig(args, "")
}

func resolveRootWithConfig(args []string, configuredPath string) (string, error) {
	var root string
	if len(args) == 1 {
		root = args[0]
	} else {
		configPath := configuredPath
		if configPath == "" {
			var err error
			configPath, err = config.FilePath()
			if err != nil {
				return "", err
			}
		}
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("%w: no Maildir root provided; pass one as an argument or create %s", errConfigurationMissing, configPath)
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

func writeWelcome(output io.Writer) error {
	if _, err := fmt.Fprintln(output, "mailtui is not configured yet."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output); err != nil {
		return err
	}
	return writeHelp(output)
}

func writeHelp(output io.Writer) error {
	configPath, err := config.FilePath()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, `mailtui — browse a Maildir backup safely and offline

USAGE
  mailtui [MAILDIR_ROOT]       Open a Maildir root directly
  mailtui --config FILE        Read a specific configuration file
  mailtui config               Show the default configuration location
  mailtui update               Install the latest GitHub release
  mailtui --version            Print the installed version
  mailtui help                 Show this guide

CONFIGURATION
  With no arguments, mailtui reads:
    %s

  Create that file with the following TOML content:
    maildir = "/path/to/your/mail"

  The configured directory must contain Maildir folders with cur/, new/,
  and tmp/ subdirectories. An explicit MAILDIR_ROOT always takes precedence.

EXAMPLES
  mailtui ~/.local/share/mail/mbsync
  mailtui --config ~/.config/mailtui/work.toml
  mailtui update

Inside the reader, press r to refresh the selected folder, / to search,
and q to quit. The Maildir is always treated as read-only.
`, configPath)
	return err
}

func showConfiguration(output io.Writer) error {
	configPath, err := config.FilePath()
	if err != nil {
		return err
	}
	fmt.Fprintln(output, configPath)
	cfg, err := config.LoadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		_, err = fmt.Fprintln(output, "Not created yet. Run 'mailtui help' for setup instructions.")
		return err
	}
	if err != nil {
		return fmt.Errorf("load configuration %s: %w", configPath, err)
	}
	_, err = fmt.Fprintf(output, "maildir = %q\n", cfg.Maildir)
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mailtui:", err)
		os.Exit(1)
	}
}
