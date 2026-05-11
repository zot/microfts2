package microfts2

import (
	"os"
	"path/filepath"
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

// R617, R618, R621: a Go raw-backtick string with embedded braces does not
// open a chunk group — the contents are scan-suppressed.
func TestBracketChunkerGoRawStringWithBraces(t *testing.T) {
	src := "x := `hello { world }`\ny := 1\n"
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
		t.Errorf("expected 1 chunk (single paragraph, no group), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// R617, R621: a plain "..."  string with embedded braces does not open a chunk
// group — the contents are scan-suppressed.
func TestBracketChunkerGoStringWithBrace(t *testing.T) {
	src := `x := "}"
y := 1
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
		t.Errorf("expected 1 chunk (no group from string brace), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// R618, R619: JS template literal with single-line ${...} interpolation should
// not form a chunk group (depth opens and closes on the same line).
func TestBracketChunkerJSTemplateSingleLine(t *testing.T) {
	src := "let s = `hello ${name} world`;\n"
	bc := BracketChunker(LangJS)
	var chunks []Chunk
	bc.Chunks("test.js", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (single paragraph), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// R618, R619, R621: a multi-line ${...} interpolation forms a chunk group
// because ${ is a code-mode bracket whose depth crosses line boundaries.
// The surrounding backticks themselves are scan-restricted and contribute no
// depth — only the ${...} interpolation does.
func TestBracketChunkerJSTemplateMultiLineInterp(t *testing.T) {
	src := "let s = `outer ${\n  inner.call(\n    arg\n  )\n}` + tail;\n"
	bc := BracketChunker(LangJS)
	var chunks []Chunk
	bc.Chunks("test.js", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	if len(chunks) < 1 {
		t.Fatalf("expected at least one group chunk, got %d", len(chunks))
	}
	// Find the multi-line group.
	foundGroup := false
	for _, c := range chunks {
		if string(c.Range) == "1-5" {
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		t.Errorf("expected a group chunk spanning lines 1-5; got: ")
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// R619: ${ is only recognized when the scanner is currently inside a backquote.
// At top level (or inside a regular "..."), ${ is just text.
func TestBracketChunkerJSDollarBraceTopLevel(t *testing.T) {
	src := "let x = ${not_a_template};\n"
	bc := BracketChunker(LangJS)
	var chunks []Chunk
	bc.Chunks("test.js", []byte(src), func(c Chunk) bool {
		chunks = append(chunks, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	// Top-level ${...} is not a recognized template construct; the line has
	// no multi-line group and should produce a single paragraph.
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (single paragraph at top level), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// R620: inside a scan-restricted group, none of the language's other openers
// are recognized — they're literal text.
func TestBracketChunkerStringDoesNotRecognizeBrackets(t *testing.T) {
	src := `func f() {
	s := "{ ( ["
	return s
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
	// One func group covering all 4 lines; the embedded "{ ( [" doesn't open
	// any new groups because the scanner is in restricted mode.
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk (the whole func), got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("  chunk %d: range=%s content=%q", i, c.Range, c.Content)
		}
	}
}

// appendCollect runs bc.AppendChunks and returns the yielded chunks (copied)
// alongside replacedLast and any error. Helper for the AppendAware tests.
func appendCollect(t *testing.T, bc AppendAwareChunker, path string, lastLocator, newBytes []byte) ([]Chunk, bool, error) {
	t.Helper()
	var out []Chunk
	replacedLast, err := bc.AppendChunks(path, lastLocator, newBytes, func(c Chunk) bool {
		out = append(out, Chunk{
			Range:   append([]byte(nil), c.Range...),
			Locator: append([]byte(nil), c.Locator...),
			Content: append([]byte(nil), c.Content...),
		})
		return true
	})
	return out, replacedLast, err
}

// R629: with no previous chunks (nil locator), AppendChunks just chunks the
// newBytes alone — no file read, no boundary fixup, replacedLast=false.
func TestBracketChunkerAppendChunksNilLocator(t *testing.T) {
	bc := BracketChunker(LangGo).(AppendAwareChunker)
	src := "func a() {\n\tone()\n}\n"
	chunks, replacedLast, err := appendCollect(t, bc, "irrelevant.go", nil, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if replacedLast {
		t.Error("replacedLast should be false when there are no previous chunks")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if string(chunks[0].Content) != src {
		t.Errorf("chunk content = %q, want %q", chunks[0].Content, src)
	}
}

// R628: when appended bytes complete the previously-partial bracket-block,
// the chunker emits a single replacement chunk and signals replacedLast=true.
func TestBracketChunkerAppendChunksCompletesGroup(t *testing.T) {
	bc := BracketChunker(LangGo).(AppendAwareChunker)

	initial := "func a() {\n\tone()\n"
	appended := "}\n"
	dir := t.TempDir()
	fp := filepath.Join(dir, "code.go")
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}

	// Previous last chunk = open group covering all of `initial`.
	oldLocator := EncodeByteRangeLocator(0, len(initial))

	chunks, replacedLast, err := appendCollect(t, bc, fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if !replacedLast {
		t.Fatal("replacedLast should be true when the first re-chunked chunk extends past the old range")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 replacement chunk, got %d", len(chunks))
	}
	if want := initial + appended; string(chunks[0].Content) != want {
		t.Errorf("chunk content = %q, want %q", chunks[0].Content, want)
	}
	if want := "1-3"; string(chunks[0].Range) != want {
		t.Errorf("chunk range = %q, want %q (absolute to full file)", chunks[0].Range, want)
	}
	start, end, ok := DecodeByteRangeLocator(chunks[0].Locator)
	if !ok {
		t.Fatal("locator did not decode")
	}
	if start != 0 || end != len(initial)+len(appended) {
		t.Errorf("locator = [%d,%d), want [0,%d)", start, end, len(initial)+len(appended))
	}
}

// R628: when the first re-chunked chunk reproduces the previous last chunk
// byte-for-byte, the chunker drops it (does not re-emit) and continues with
// the appended chunks, signalling replacedLast=false.
func TestBracketChunkerAppendChunksCleanBoundary(t *testing.T) {
	bc := BracketChunker(LangGo).(AppendAwareChunker)

	// Two complete functions separated by a blank line, then append a third.
	initial := "func a() {\n\tone()\n}\n\nfunc b() {\n\ttwo()\n}\n"
	appended := "\nfunc c() {\n\tthree()\n}\n"
	dir := t.TempDir()
	fp := filepath.Join(dir, "code.go")
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}

	// Previous last chunk = func b's group. Its byte range in the file is the
	// span starting at "func b() {" through its closing "}\n".
	oldStart := len("func a() {\n\tone()\n}\n\n")
	oldEnd := len(initial)
	oldLocator := EncodeByteRangeLocator(oldStart, oldEnd)

	chunks, replacedLast, err := appendCollect(t, bc, fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if replacedLast {
		t.Error("replacedLast should be false when the first re-chunked chunk matches the previous range")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 new chunk (func c), got %d", len(chunks))
	}
	if want := "func c() {\n\tthree()\n}\n"; string(chunks[0].Content) != want {
		t.Errorf("new chunk content = %q, want %q", chunks[0].Content, want)
	}
}

// R627: extending a paragraph with appended comment lines is detected by
// the re-chunk-from-prior-start strategy — the merged paragraph replaces
// the previous chunk.
func TestBracketChunkerAppendChunksExtendsParagraph(t *testing.T) {
	bc := BracketChunker(LangGo).(AppendAwareChunker)

	initial := "// comment 1\n// comment 2\n"
	appended := "// comment 3\n"
	dir := t.TempDir()
	fp := filepath.Join(dir, "code.go")
	if err := os.WriteFile(fp, []byte(initial+appended), 0644); err != nil {
		t.Fatal(err)
	}

	// Previous last chunk = the 2-line comment paragraph.
	oldLocator := EncodeByteRangeLocator(0, len(initial))

	chunks, replacedLast, err := appendCollect(t, bc, fp, oldLocator, []byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	if !replacedLast {
		t.Fatal("replacedLast should be true when the paragraph is extended")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 merged paragraph chunk, got %d", len(chunks))
	}
	if want := initial + appended; string(chunks[0].Content) != want {
		t.Errorf("merged chunk content = %q, want %q", chunks[0].Content, want)
	}
}
