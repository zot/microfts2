# Chunker
**Requirements:** R10, R11, R12, R13, R116, R128, R129, R130, R131, R169, R170, R171, R172, R173, R174, R177, R291, R292, R293, R294, R295, R296, R465, R466, R467, R468

Provides the Chunker interface (Chunks + ChunkText), the FuncChunker adapter, the Pair type, and built-in chunker implementations. A chunking strategy is a name mapped to a Chunker (Go interface) or a shell command.

## Knows
- Chunker interface: Chunks(path, content, yield) + ChunkText(path, content, rangeLabel)
- Chunk struct: Range, Content, Attrs []Pair
- Pair struct: Key []byte, Value []byte — opaque key-value pair
- ChunkFunc type: convenience function type for simple chunkers
- FuncChunker: adapter wrapping ChunkFunc into Chunker interface

## Does
- FuncChunker.Chunks(path, content, yield): delegate to wrapped ChunkFunc
- FuncChunker.ChunkText(path, content, rangeLabel): re-run wrapped ChunkFunc, return first chunk whose Range matches rangeLabel
- RunChunkerFunc(cmd): return a ChunkFunc that executes `cmd filepath`, parses `range\tcontent` lines from stdout, and yields Chunk structs via the generator pattern
- MarkdownChunkFunc(path, content, yield): paragraph-based markdown chunker — heading lines start new chunks, heading + following paragraph form one chunk, consecutive blank lines collapse, range is `startline-endline`. Fenced code blocks (``` or ~~~) suppress blank-line splitting: all lines from opening fence through matching close belong to the current chunk. Fence opening continues the current chunk, does not start a new one.

## Collaborators
- none (leaf type, executes external processes or processes content directly)

## Sequences
- seq-add.md
- seq-chunks.md
