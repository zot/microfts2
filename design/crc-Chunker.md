# Chunker
**Requirements:** R10, R11, R12, R13, R116, R128, R129, R130, R131, R169, R170, R171, R172, R173, R174, R177, R291, R292, R293, R294, R295, R296, R465, R466, R467, R468, R502, R503, R504, R505, R506, R507, R508, R509, R510, R511, R512, R517, R518, R519, R520, R521, R522, R523

Provides the chunking interfaces (Chunker, FileChunker, ChunkTexter), the FuncChunker adapter, the Pair type, and built-in chunker implementations. A chunking strategy is a name mapped to a chunker (any combination of interfaces) or a shell command.

## Knows
- Chunker interface: Chunks(path string, content []byte, yield func(Chunk) bool) error — content-based chunking for text formats
- FileChunker interface: FileChunks(path string, old [32]byte, yield func(Chunk) bool) ([32]byte, error) — file-based chunking for binary formats. Distinct method name avoids collision with Chunker.Chunks so a single type can implement both
- ChunkTexter interface: ChunkText(path string, content []byte, rangeLabel string) ([]byte, bool) — optional optimized single-chunk retrieval
- Chunk struct: Range, Content, Attrs []Pair
- Pair struct: Key []byte, Value []byte — opaque key-value pair
- ChunkFunc type: convenience function type for simple chunkers
- FuncChunker: adapter wrapping ChunkFunc into Chunker + ChunkTexter

## Does
- FuncChunker.Chunks(path, content, yield): delegate to wrapped ChunkFunc
- FuncChunker.ChunkText(path, content, rangeLabel): re-run wrapped ChunkFunc, return first chunk whose Range matches rangeLabel
- chunkTextByRange(c, path, content, rangeLabel): default ChunkText for Chunker without ChunkTexter — re-run Chunks, stop at match
- chunkTextByRangeFile(fc, path, rangeLabel): default ChunkText for FileChunker without ChunkTexter — re-run FileChunker.FileChunks(path, zero, yield), stop at match
- wantsContent(c): helper — returns true if c implements Chunker (content-based), false if only FileChunker
- resolveChunkText(c, path, content, rangeLabel): dispatch helper — ChunkTexter → direct; Chunker → chunkTextByRange; FileChunker → chunkTextByRangeFile
- RunChunkerFunc(cmd): return a ChunkFunc that executes `cmd filepath`, parses `range\tcontent` lines from stdout, and yields Chunk structs via the generator pattern
- MarkdownChunkFunc(path, content, yield): paragraph-based markdown chunker — heading lines start new chunks, heading + following paragraph form one chunk, consecutive blank lines collapse, range is `startline-endline`. Fenced code blocks (``` or ~~~) suppress blank-line splitting: all lines from opening fence through matching close belong to the current chunk. Fence opening continues the current chunk, does not start a new one.

## Collaborators
- none (leaf type, executes external processes or processes content directly)

## Sequences
- seq-add.md
- seq-chunks.md
