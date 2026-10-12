package chunker

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStableIDDeterministicAndUUIDShaped(t *testing.T) {
	c := Chunk{Repo: "r", Path: "internal/foo.go", Symbol: "*Store.Get"}
	id1 := c.StableID()
	id2 := c.StableID()
	if id1 != id2 {
		t.Fatalf("expected stable id, got %s vs %s", id1, id2)
	}
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !re.MatchString(id1) {
		t.Fatalf("id %q is not UUID-shaped", id1)
	}
	other := Chunk{Repo: "r", Path: "internal/foo.go", Symbol: "Store.Get"}
	if other.StableID() == id1 {
		t.Fatal("pointer vs value receiver should not collide")
	}
}

func TestChunkFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	src := `package sample

import "context"

// Get loads a record.
func (s *Store[T]) Get(ctx context.Context) error {
	return nil
}

type Store[T any] struct{}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := ChunkFile("repo", path, "sample.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	var sawMethod, sawType bool
	for _, c := range chunks {
		switch c.Kind {
		case "method":
			sawMethod = true
			if c.Symbol != "*Store.Get" {
				t.Fatalf("method symbol = %q", c.Symbol)
			}
			if !strings.Contains(c.Text(), "package sample") {
				t.Fatal("embed text missing package")
			}
		case "type":
			sawType = true
			if c.Symbol != "Store" {
				t.Fatalf("type symbol = %q", c.Symbol)
			}
		}
	}
	if !sawMethod || !sawType {
		t.Fatalf("missing expected kinds: %+v", chunks)
	}
}
