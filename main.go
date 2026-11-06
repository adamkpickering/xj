package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/philippgille/chromem-go"
	"github.com/urfave/cli/v3"
)

const filenameFormat = "2006-01-02.xj"

func main() {
	cmd := &cli.Command{
		Name:  "xj",
		Usage: "A simple tool for version-controllable work journals",
		Commands: []*cli.Command{
			{
				Name:   "new",
				Usage:  "Create a new journal entry for today",
				Action: newEntry,
			},
			{
				Name:  "rag",
				Usage: "Commands pertaining to RAG",
				Commands: []*cli.Command{
					{
						Name:   "sync",
						Usage:  "Construct vector database",
						Action: ragSync,
					},
					{
						Name:   "query",
						Usage:  "Run vector search",
						Action: ragQuery,
					},
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// chunkText splits text into chunks of at most maxChars, preserving paragraph
// and line boundaries where possible. Falls back to hard-splitting at maxChars
// if a single line is too long.
func chunkText(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	var b strings.Builder

	// Helper: flush current builder to a chunk.
	flush := func() {
		if b.Len() > 0 {
			chunks = append(chunks, b.String())
			b.Reset()
		}
	}

	// Helper: add text to builder, flushing first if it won't fit.
	add := func(sep, s string) {
		need := len(sep) + len(s)
		if b.Len()+need > maxChars && b.Len() > 0 {
			flush()
		}
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
	}

	paragraphs := strings.Split(text, "\n\n")
	for _, para := range paragraphs {
		// If paragraph itself fits, try to add it as a whole.
		if len(para) <= maxChars {
			add("\n\n", para)
			continue
		}

		// Paragraph too long: split on single newlines first.
		lines := strings.Split(para, "\n")
		for _, line := range lines {
			if len(line) <= maxChars {
				add("\n", line)
				continue
			}

			// Line itself too long: hard-split at maxChars.
			for len(line) > 0 {
				n := maxChars
				if n > len(line) {
					n = len(line)
				}
				// Try not to cut words in half.
				if n < len(line) {
					if i := strings.LastIndex(line[:n], " "); i != -1 {
						n = i + 1 // include the space
					}
				}
				add("", strings.TrimSpace(line[:n]))
				line = strings.TrimSpace(line[n:])
			}
		}
	}
	flush()
	return chunks
}

func load(dbPath string) (*chromem.DB, *chromem.Collection, error) {
	db := chromem.NewDB()
	if err := db.ImportFromFile(dbPath, ""); err != nil {
		return nil, nil, fmt.Errorf("failed to import database: %w", err)
	}

	embeddingFunc := chromem.NewEmbeddingFuncOllama("nomic-embed-text", "http://127.0.0.1:11434/api")
	collection, err := db.GetOrCreateCollection("work-journal", nil, embeddingFunc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get or create collection: %w", err)
	}

	return db, collection, nil
}

func ragSync(ctx context.Context, cmd *cli.Command) error {
	dbPath := "chromem-go"

	db, collection, err := load(dbPath)
	if err != nil {
		return fmt.Errorf("failed to load collection: %w", err)
	}

	entries, err := filepath.Glob("/home/adam/projects/work-journal/default/*.xj")
	if err != nil {
		return fmt.Errorf("failed to list .xj files: %w", err)
	}

	for i, entry := range entries {
		content, err := os.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("failed to read %q: %w", entry, err)
		}
		chunks := chunkText(string(content), 256)
		for j, chunk := range chunks {
			fmt.Printf("Adding document for chunk %d of %s\n", j, entry)
			document := chromem.Document{
				ID:      fmt.Sprintf("%d-%d", i, j),
				Content: chunk,
				Metadata: map[string]string{
					"path": entry,
				},
			}
			if err := collection.AddDocument(ctx, document); err != nil {
				return fmt.Errorf("failed to add document for chunk %d of %s: %w", j, entry, err)
			}
		}
	}

	if err := db.ExportToFile(dbPath, false, ""); err != nil {
		return fmt.Errorf("failed to export db: %w", err)
	}

	return nil
}

func ragQuery(ctx context.Context, cmd *cli.Command) error {
	_, collection, err := load("chromem-go")
	if err != nil {
		return fmt.Errorf("failed to load collection: %w", err)
	}

	results, err := collection.Query(ctx, "Find instances of swearing.", 2, nil, nil)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	for _, result := range results {
		fmt.Printf("%s\n\n", result.Content)
	}

	return nil
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
