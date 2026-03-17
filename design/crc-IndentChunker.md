# IndentChunker
**Requirements:** R325, R326, R327, R328, R329, R330, R331, R332, R333, R334, R335

Chunker for indentation-scoped languages. Reuses BracketLang for comment/string config (Brackets field ignored). Scope determined by indentation level changes.

## Knows
- BracketLang: comment and string rules (Brackets ignored)
- tabWidth: how tabs count for column calculation (0 = one column per tab)

## Does
- IndentChunker(lang, tabWidth) Chunker: factory returning a Chunker for indentation-scoped languages
- Chunks(path, content, yield): scan lines, track indentation levels, identify groups (indent increase) and paragraphs, yield chunks with startline-endline ranges
- ChunkText(path, content, rangeLabel): scan to target range and return its content
- measureIndent(line, tabWidth): count leading whitespace columns

## Collaborators
- Chunker interface (implements it)
- BracketLang (reuses comment/string config from BracketChunker)

## Sequences
- seq-indent-chunk.md
