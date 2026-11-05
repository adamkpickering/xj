package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

const filenameFormat = "2006-01-02.xj"

func main() {
	cmd := &cli.Command{
		Name:   "xj",
		Usage:  "A simple tool for version-controllable work journals",
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{
			{
				Name:   "new",
				Usage:  "Create a new journal entry for today",
				Action: newEntry,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newEntry(_ context.Context, cmd *cli.Command) error {
	todayFilename := time.Now().Format(filenameFormat)

	entries, err := filepath.Glob("*.xj")
	if err != nil {
		return fmt.Errorf("failed to list .xj files: %w", err)
	}

	// If there are no existing .xj files, create a default template.
	if len(entries) == 0 {
		return os.WriteFile(todayFilename, []byte("To Do:\n\nWorked On:\n"), 0644)
	}

	// If today's entry already exists, error out.
	for _, entry := range entries {
		if entry == todayFilename {
			return fmt.Errorf("there is already an entry for today")
		}
	}

	// Collect entries that match the date format and find the latest one.
	type entryWithDate struct {
		name string
		date time.Time
	}

	var datedEntries []entryWithDate
	for _, entry := range entries {
		t, err := time.Parse(filenameFormat, entry)
		if err != nil {
			// Skip files that don't match the expected filename format.
			continue
		}
		datedEntries = append(datedEntries, entryWithDate{name: entry, date: t})
	}

	// If none of the existing files match the date pattern, fall back to the default template.
	if len(datedEntries) == 0 {
		return os.WriteFile(todayFilename, []byte("To Do:\n\nWorked On:\n"), 0644)
	}

	slices.SortFunc(datedEntries, func(a, b entryWithDate) int {
		return b.date.Compare(a.date)
	})

	latestFilename := datedEntries[0].name
	latestContent, err := os.ReadFile(latestFilename)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", latestFilename, err)
	}

	// Split into lines, mimicking Nushell's `lines` behavior which drops a trailing empty line.
	lines := strings.Split(string(latestContent), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Find "Worked On:" and take everything up to and including that line.
	untilIndex := len(lines)
	for i, line := range lines {
		if line == "Worked On:" {
			untilIndex = i + 1
			break
		}
	}

	selected := lines[:untilIndex]
	output := strings.Join(selected, "\n") + "\n"

	return os.WriteFile(todayFilename, []byte(output), 0644)
}
