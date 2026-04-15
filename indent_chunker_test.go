package microfts2

import (
	"testing"
)

func TestIndentChunkerPythonBasic(t *testing.T) {
	src := `import os

def hello():
    print("hello")
    print("world")

def goodbye():
    print("bye")

x = 1
`
	lang := BracketLang{
		LineComments: []string{"#"},
		StringDelims: []StringDelim{
			{Open: `"`, Close: `"`, Escape: `\`},
			{Open: "'", Close: "'", Escape: `\`},
			{Open: `"""`, Close: `"""`},
			{Open: "'''", Close: "'''"},
		},
	}
	ic := IndentChunker(lang, 4)
	var chunks []Chunk
	err := ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	// Expect: "import os" paragraph, "def hello" group, "def goodbye" group, "x = 1" paragraph
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		t.Logf("chunk %d: range=%s content=%q", i, c.Range, c.Content)
	}
}

func TestIndentChunkerNestedIndent(t *testing.T) {
	// Nested indentation should be part of the outer group
	src := `def outer():
    if True:
        inner()
    return
`
	lang := BracketLang{LineComments: []string{"#"}}
	ic := IndentChunker(lang, 4)
	var chunks []Chunk
	ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (nested inside outer), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

func TestIndentChunkerYAML(t *testing.T) {
	src := `name: app
version: 1.0

server:
  host: localhost
  port: 8080

database:
  driver: postgres
  name: mydb
`
	lang := BracketLang{LineComments: []string{"#"}}
	ic := IndentChunker(lang, 2)
	var chunks []Chunk
	ic.Chunks("test.yaml", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	// Expect: "name/version" paragraph, "server" group, "database" group
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		t.Logf("chunk %d: range=%s content=%q", i, c.Range, c.Content)
	}
}

func TestIndentChunkerLeadingComment(t *testing.T) {
	// R330: leading comment attaches to group
	src := `# This function greets
def greet():
    print("hi")
`
	lang := BracketLang{LineComments: []string{"#"}}
	ic := IndentChunker(lang, 4)
	var chunks []Chunk
	ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (comment + def), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
	if len(chunks) > 0 && string(chunks[0].Range) != "1-3" {
		t.Errorf("range = %s, want 1-3", chunks[0].Range)
	}
}

func TestIndentChunkerChunkText(t *testing.T) {
	src := `def a():
    pass

def b():
    pass
`
	lang := BracketLang{LineComments: []string{"#"}}
	ic := IndentChunker(lang, 4)

	var ranges []string
	ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		ranges = append(ranges, string(c.Range))
		return true
	})

	if len(ranges) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(ranges))
	}

	ct := ic.(ChunkTexter)
	text, ok := ct.ChunkText("test.py", []byte(src), ranges[1])
	if !ok {
		t.Fatal("ChunkText returned false")
	}
	if len(text) == 0 {
		t.Error("ChunkText returned empty")
	}
}

func TestIndentChunkerEmpty(t *testing.T) {
	lang := BracketLang{}
	ic := IndentChunker(lang, 4)
	var chunks []Chunk
	err := ic.Chunks("test.py", nil, func(c Chunk) bool {
		chunks = append(chunks, c)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestIndentChunkerTabWidth(t *testing.T) {
	// R328: tab width
	src := "def f():\n\tpass\n"
	lang := BracketLang{}

	// tabWidth=0: tab counts as 1 column
	ic := IndentChunker(lang, 0)
	var chunks []Chunk
	ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if len(chunks) != 1 {
		t.Errorf("tabWidth=0: expected 1 chunk, got %d", len(chunks))
	}

	// tabWidth=8
	ic = IndentChunker(lang, 8)
	chunks = nil
	ic.Chunks("test.py", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if len(chunks) != 1 {
		t.Errorf("tabWidth=8: expected 1 chunk, got %d", len(chunks))
	}
}

func TestMeasureIndent(t *testing.T) {
	tests := []struct {
		line     string
		tabWidth int
		want     int
	}{
		{"hello", 4, 0},
		{"  hello", 4, 2},
		{"\thello", 4, 4},
		{"\thello", 0, 1},
		{"  \thello", 4, 4}, // 2 spaces + tab rounds to next tab stop
		{"\t\thello", 4, 8},
	}
	for _, tt := range tests {
		got := measureIndent([]byte(tt.line), tt.tabWidth)
		if got != tt.want {
			t.Errorf("measureIndent(%q, %d) = %d, want %d", tt.line, tt.tabWidth, got, tt.want)
		}
	}
}
