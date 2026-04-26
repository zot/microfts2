# Chunker
**Requirements:** R10, R11, R12, R13, R116, R128, R129, R130, R131, R169, R170, R171, R172, R173, R174, R177, R291, R292, R293, R294, R295, R296, R465, R466, R467, R468, R502, R505, R506, R507, R508, R509, R510, R511, R512, R518, R519, R520, R524, R525, R526, R527, R528, R529, R530, R531, R532, R533, R534, R563, R564, R565, R566, R567, R568, R569, R570, R587, R588, R589, R590, R591, R593, R601, R602, R603, R607, R610

Provides the chunking interfaces (Chunker, FileChunker, RandomAccessChunker), the FuncChunker adapter, the Pair type, and built-in chunker implementations. A chunking strategy is a name mapped to a chunker (any combination of interfaces) or a shell command.

## Knows
- Chunker interface: Chunks(path string, content []byte, yield func(Chunk) bool) error — content-based chunking for text formats
- FileChunker interface: FileChunks(path string, old [32]byte, yield func(Chunk) bool) ([32]byte, error) — file-based chunking for binary formats. Distinct method name avoids collision with Chunker.Chunks so a single type can implement both
- RandomAccessChunker interface: GetChunk(path string, data []byte, customData *any, chunk *Chunk) error — optional fast-path retrieval for a single chunk by pre-filled range
- AppendAwareChunker interface: AppendChunks(path string, lastLocator []byte, newBytes []byte, yield func(Chunk) bool) (replacedLast bool, err error) — optional boundary-aware append for chunkers whose tail-chunk shape depends on prior content
- Chunk struct: Range []byte, Locator []byte, Content []byte, Attrs []Pair — Locator is per-occurrence (stored on F record, not C record), opaque chunker-defined bytes used for fast retrieval and append-merge resume; nil when not needed
- Pair struct: Key []byte, Value []byte — opaque key-value pair
- ChunkFunc type: convenience function type for simple chunkers
- FuncChunker: adapter wrapping ChunkFunc into Chunker

## Does
- FuncChunker.Chunks(path, content, yield): delegate to wrapped ChunkFunc
- wantsContent(c): helper — returns true if c implements Chunker (content-based), false if only FileChunker
- RunChunkerFunc(cmd): return a ChunkFunc that executes `cmd filepath`, parses `range\tcontent` lines from stdout, and yields Chunk structs via the generator pattern
- MarkdownChunkFunc(path, content, yield): paragraph-based markdown chunker — heading lines start new chunks, heading + following paragraph form one chunk, consecutive blank lines collapse, range is `startline-endline`. Fenced code blocks (``` or ~~~) suppress blank-line splitting: all lines from opening fence through matching close belong to the current chunk. Fence opening continues the current chunk, does not start a new one. Headline merging: after producing a heading chunk, absorbs consecutive tag-only chunks (every line starts with `@`) and then one non-heading content chunk. Internal blank lines become part of the merged chunk's content. Range spans the full merged region.
- RandomAccessChunker.GetChunk contract: caller pre-fills chunk.Range (from F record Location) and chunk.Attrs (from C record stored Attrs). Chunker fills chunk.Content; may replace or augment chunk.Attrs; may use stored Attrs as retrieval hints (e.g. a stored "page-offset" attr). Uses customData as per-file scratch (e.g. line-offset table); *customData == nil on first call, chunker populates lazily.
- Line-offset retrofit (built-in chunkers): LineChunk, MarkdownChunkFunc, BracketChunker, IndentChunker each implement RandomAccessChunker using `[]int` line-offset table stored in customData. Incrementally extended — scans from last-known offset up to requested line on demand. Given range "startLine-endLine", slice data[offsets[startLine-1]:offsets[endLine]] to produce Content. No depth/indent state needed because stored range label identifies byte region.
- AppendAwareChunker contract: chunker yields chunks that replace the trailing region of the file. When replacedLast=true, the dispatcher drops the F record's last chunk entry and decrements its chunkid's fileid count in the C record, cascading orphan cleanup if that was the last reference. Chunkers that don't implement AppendAwareChunker get the default behavior in DB.AppendChunks: chunk newBytes alone, append without boundary fixup.
- Locator usage by built-in chunkers: line-oriented chunkers store byte-range coordinates so random-access retrieval can slice directly without rebuilding line-offset tables. Tree-format chunkers (markdown, bracket, indent) encode resume state — the leaf-vs-inner-with-absorbed-leaf shape of the last chunk plus structural depth (heading level, indent depth, bracket depth) — for AppendAwareChunker.AppendChunks to read.

## Collaborators
- none (leaf type, executes external processes or processes content directly)

## Sequences
- seq-add.md
- seq-chunks.md
- seq-chunker-dispatch.md
