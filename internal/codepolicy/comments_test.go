package codepolicy

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	allowedDirective = regexp.MustCompile(`^//(go:[a-z]+|line |extern |export )`)
	generatedMarker  = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	skipDirs         = map[string]bool{".git": true, "testdata": true, "vendor": true, "bin": true, "dist": true}
)

func TestNoCommentsInGoCode(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if allowedDirective.MatchString(comment.Text) || generatedMarker.MatchString(comment.Text) {
					continue
				}
				line := fset.Position(comment.Pos()).Line
				t.Errorf("%s:%d: comments are forbidden (found %q); see AGENTS.md", rel, line, firstLine(comment.Text))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk source tree: %v", walkErr)
	}
}

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return before
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}
