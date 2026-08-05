# Indent Chunker

A chunker for languages where indentation defines scope. Similar to the bracket chunker — groups and paragraphs — but scope is determined by indentation level rather than bracket characters.

## Languages

Python, YAML, and potentially other indentation-scoped formats (Haskell, CoffeeScript, Nim, Makefiles).

## Token types

Same as bracket chunker (comment, string, whitespace, text) minus brackets. Comment and string syntax is still configurable per language, using the same `BracketLang` structure (with empty `Brackets`).

## Scope detection

- **Indent increase**: a line indented further than the previous non-blank line opens a new scope.
- **Dedent**: a line at a lower indentation level than the current scope closes the scope.
- **Tabs vs spaces**: configurable per language (tab width for column counting). Mixed indentation uses the configured tab width.

## Chunk definition

- **Group**: a line that introduces a deeper indentation level (the "header" line), plus all following lines at that deeper level or deeper, until dedent. Leading comment lines attach to the group (same rule as bracket chunker).
- **Paragraph**: consecutive lines at the same indentation level (the top level or between groups), terminated by a blank line or the start of a group.

Range labels are `startline-endline` (1-based).

## Library API

```go
// IndentChunker returns a Chunker for indentation-scoped languages.
// tabWidth controls how tabs are counted for indentation level (0 = tabs are one column).
func IndentChunker(lang BracketLang, tabWidth int) Chunker
```

Reuses `BracketLang` for comment/string config (Brackets field ignored).

## CLI

```
microfts chunk-indent -lang <name> [-tabwidth N] <file>
```

Output: same `range\tcontent` format.
