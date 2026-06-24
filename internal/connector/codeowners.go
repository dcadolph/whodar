package connector

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoCodeOwners indicates no CODEOWNERS file was found.
var ErrNoCodeOwners = errors.New("codeowners: no CODEOWNERS file found")

// codeStop are path segments and file extensions too generic to be topics.
var codeStop = map[string]bool{
	"internal": true, "src": true, "pkg": true, "cmd": true, "lib": true,
	"test": true, "tests": true, "common": true, "util": true, "utils": true,
	"core": true, "app": true, "apps": true, "main": true, "vendor": true,
	"dist": true, "build": true, "docs": true, "doc": true, "node_modules": true,
	"go": true, "js": true, "ts": true, "jsx": true, "tsx": true, "py": true,
	"rb": true, "java": true, "md": true, "txt": true, "yaml": true, "yml": true,
	"json": true, "html": true, "css": true, "sh": true, "tf": true, "sql": true,
}

// CodeOwners is a Source that reads a CODEOWNERS file and maps each owner to the
// topics implied by the paths they own. It answers "who owns this system".
type CodeOwners struct {
	// Path is a CODEOWNERS file or a repo root to search for one.
	Path string
}

// NewCodeOwners returns a CodeOwners source for path.
func NewCodeOwners(path string) *CodeOwners {
	return &CodeOwners{Path: path}
}

// Fetch finds and parses the CODEOWNERS file, returning one record per owner.
func (c *CodeOwners) Fetch(ctx context.Context) ([]Record, error) {
	path, err := findCodeOwners(c.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("codeowners: open: %w", err)
	}
	defer f.Close()
	return parseCodeOwners(ctx, f)
}

// findCodeOwners returns path when it is a file, or searches the standard
// locations when it is a directory.
func findCodeOwners(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("codeowners: stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, rel := range []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"} {
		cand := filepath.Join(path, rel)
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%w in %s", ErrNoCodeOwners, path)
}

// parseCodeOwners reads CODEOWNERS lines and returns one record per owner, in
// first-seen order.
func parseCodeOwners(ctx context.Context, r io.Reader) ([]Record, error) {
	patterns := make(map[string][]string)
	var order []string

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, owner := range fields[1:] {
			if patterns[owner] == nil {
				order = append(order, owner)
			}
			patterns[owner] = append(patterns[owner], fields[0])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("codeowners: scan: %w", err)
	}

	records := make([]Record, 0, len(order))
	for _, owner := range order {
		records = append(records, ownerRecord(owner, patterns[owner]))
	}
	return records, nil
}

// ownerRecord builds a record for an owner. An email owner joins other sources
// by email; an @handle or @org/team becomes its own contact entry.
func ownerRecord(owner string, patterns []string) Record {
	rec := Record{
		Kind:   KindPerson,
		Source: "codeowners",
		Weight: 1,
		Name:   owner,
		Topics: topicsFromPatterns(patterns),
	}
	if strings.HasPrefix(owner, "@") {
		rec.PersonID = "codeowners:" + strings.ToLower(strings.TrimPrefix(owner, "@"))
	} else if strings.Contains(owner, "@") {
		rec.Email = strings.ToLower(owner)
	} else {
		rec.PersonID = "codeowners:" + strings.ToLower(owner)
	}
	return rec
}

// topicsFromPatterns derives topic tags from the path segments of patterns,
// dropping generic directory names and file extensions.
func topicsFromPatterns(patterns []string) []string {
	seen := make(map[string]bool)
	var topics []string
	for _, p := range patterns {
		for _, seg := range strings.Split(p, "/") {
			seg = strings.Trim(seg, "*?.")
			if seg == "" {
				continue
			}
			for _, part := range strings.Split(seg, ".") {
				part = strings.ToLower(strings.TrimSpace(part))
				if len(part) < 3 || codeStop[part] {
					continue
				}
				if !seen[part] {
					seen[part] = true
					topics = append(topics, part)
				}
			}
		}
	}
	return topics
}
