# IndentChunker
**Requirements:** R325, R326, R327, R328, R329, R330, R331, R332, R333, R334, R335, R633, R636, R637, R638

Chunker for indentation-scoped languages. Reuses BracketLang for comment/string config (Brackets field ignored). Scope determined by indentation level changes. Implements the full text-chunker quartet: Chunker, FileChunker (via fileChunksByRead), RandomAccessChunker, and AppendAwareChunker (via appendByRechunkResume).

## Knows
- BracketLang: comment rules (LineComments, BlockComments) — Brackets field ignored; with the unified BracketGroup model, "string" config also lives in Brackets and is therefore ignored here
- tabWidth: how tabs count for column calculation (0 = one column per tab)

## Does
- IndentChunker(lang, tabWidth) Chunker: factory returning a Chunker (also implements FileChunker, RandomAccessChunker, and AppendAwareChunker) for indentation-scoped languages
- Chunks(path, content, yield): scan lines, track indentation levels, identify groups (indent increase) and paragraphs, yield chunks with startline-endline ranges
- FileChunks(path, old, yield): read file, hash, skip-if-match-old, otherwise delegate to Chunks — delegates to shared `fileChunksByRead` helper (R633, R636)
- ChunkText(path, content, rangeLabel): scan to target range and return its content
- AppendChunks(path, lastLocator, newBytes, yield): delegates to shared `appendByRechunkResume(path, lastLocator, newBytes, ic.Chunks, yield)` helper (R633, R637, R638) — indent-scope boundaries are recognised across the append by re-chunking from the previous last chunk's start through EOF; the first emitted chunk's byte range determines whether the previous tail was clean (drop, replacedLast=false) or extended (replace, replacedLast=true)
- measureIndent(line, tabWidth): count leading whitespace columns

## Collaborators
- Chunker, FileChunker, RandomAccessChunker, AppendAwareChunker interfaces (implements them)
- BracketLang (reuses comment/string config from BracketChunker)
- fileChunksByRead, appendByRechunkResume (shared helpers in chunker.go)

## Sequences
- seq-indent-chunk.md
