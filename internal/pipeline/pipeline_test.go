package pipeline

import "testing"

func TestSkipDir(t *testing.T) {
	tests := []struct {
		name   string
		ignore []string
		want   bool
	}{
		{".git", nil, true},
		{"vendor", nil, true},
		{"generated", []string{"generated", "mocks"}, true},
		{"cmd", []string{"generated"}, false},
		{"internal", nil, false},
	}
	for _, tt := range tests {
		if got := SkipDir(tt.name, tt.ignore); got != tt.want {
			t.Errorf("SkipDir(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsGoSource(t *testing.T) {
	if !IsGoSource("foo.go") {
		t.Fatal("foo.go should be indexed")
	}
	if IsGoSource("foo_test.go") {
		t.Fatal("tests should be skipped")
	}
	if IsGoSource("readme.md") {
		t.Fatal("non-go should be skipped")
	}
}
