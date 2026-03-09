# Chunker
**Requirements:** R10, R11, R12, R13, R128, R129, R130, R131, R169, R170, R171, R172, R173, R174, R177

Wraps external chunking commands as ChunkFunc generators and provides built-in ChunkFunc implementations. A chunking strategy is a name mapped to a shell command or a Go function.

## Knows
- (stateless — strategies stored in DB settings)

## Does
- RunChunkerFunc(cmd): return a ChunkFunc that executes `cmd filepath`, parses `range\tcontent` lines from stdout, and yields Chunk structs via the generator pattern
- MarkdownChunkFunc(path, content, yield): paragraph-based markdown chunker — heading lines start new chunks, heading + following paragraph form one chunk, consecutive blank lines collapse, range is `startline-endline`

## Collaborators
- none (leaf type, executes external processes or processes content directly)

## Sequences
- seq-add.md
