package microfts2

import (
	"testing"
)

func TestBracketChunkerGoBasic(t *testing.T) {
	src := `package main

import "fmt"

func hello() {
	fmt.Println("hello")
}

func world() {
	fmt.Println("world")
}
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	err := bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should get: "package main" paragraph, "import" group, "func hello" group, "func world" group
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// First chunk should be the "package main" paragraph
	if string(chunks[0].Range) != "1-1" {
		t.Errorf("first chunk range = %s, want 1-1", chunks[0].Range)
	}
}

func TestBracketChunkerCommentInString(t *testing.T) {
	// R311: comments inside strings are not comments
	src := `x = "// not a comment"
y = 1
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	// Should be one paragraph — the string should not split the text
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

func TestBracketChunkerNestedBrackets(t *testing.T) {
	// R316: nested groups are part of outer group
	src := `func outer() {
	if true {
		inner()
	}
}
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (nested group), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

func TestBracketChunkerParagraphAndGroup(t *testing.T) {
	// R318: paragraph terminated by group start
	src := `package main

var x = 1
var y = 2

func f() {
	return
}
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	// Expect: "package main", "var x / var y", "func f" group
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

func TestBracketChunkerLeadingComment(t *testing.T) {
	// R317: leading comment attaches to group
	src := `// greet prints a greeting.
func greet() {
	println("hi")
}
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (comment + func), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
	if len(chunks) > 0 && string(chunks[0].Range) != "1-4" {
		t.Errorf("chunk range = %s, want 1-4", chunks[0].Range)
	}
}

func TestBracketChunkerWordBrackets(t *testing.T) {
	// R313, R314: word brackets like begin/end
	src := `program main;
begin
  writeln('hello');
end.
`
	bc := BracketChunker(LangPascal)
	var chunks []Chunk
	bc.Chunks("test.pas", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}
}

func TestBracketChunkerShellIfThenFi(t *testing.T) {
	// Shell word brackets: if/then/else/fi
	src := `#!/bin/bash

if [ -f file ]; then
  echo "exists"
else
  echo "missing"
fi

echo "done"
`
	bc := BracketChunker(LangShell)
	var chunks []Chunk
	bc.Chunks("test.sh", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	// Expect: shebang paragraph, if/fi group, echo paragraph
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestBracketChunkerMultiLineParams(t *testing.T) {
	// Multi-line parens followed by brace — one group, not two
	src := `func fred(
	x int,
	y int,
) {
	return x + y
}
`
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	bc.Chunks("test.go", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (multi-line func), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
	if len(chunks) > 0 && string(chunks[0].Range) != "1-6" {
		t.Errorf("chunk range = %s, want 1-6", chunks[0].Range)
	}
}

func TestBracketChunkerEmpty(t *testing.T) {
	bc := BracketChunker(LangGo)
	var chunks []Chunk
	err := bc.Chunks("test.go", nil, func(c Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestLangByName(t *testing.T) {
	// R321: built-in registry
	for _, name := range []string{"go", "c", "cpp", "java", "js", "lisp", "nginx", "pascal", "shell", "bash"} {
		if _, ok := LangByName(name); !ok {
			t.Errorf("LangByName(%q) returned false", name)
		}
	}
	if _, ok := LangByName("nonexistent"); ok {
		t.Error("LangByName(nonexistent) returned true")
	}
}
