# BracketChunker
**Requirements:** R307, R309, R310, R311, R312, R313, R314, R315, R316, R317, R318, R319, R320, R321, R322, R323, R324, R617, R618, R619, R620, R621, R622, R626, R627, R628, R629, R630, R633, R636

Configurable chunker that groups program text into chunks based on bracket structure. Table-driven — one BracketLang config per language. Strings, code brackets, and word brackets share one shape (`BracketGroup`); the string-vs-bracket distinction is replaced by a scan-mode flag (nil vs non-nil `AllowedInner`). Implements the full text-chunker quartet: Chunker, FileChunker (via fileChunksByRead), RandomAccessChunker, and AppendAwareChunker (via appendByRechunkResume).

## Knows
- BracketLang: language-specific lexical rules (line/block comments, bracket groups)
- BracketGroup: open/separator/close sets plus Escape, AllowedInner, AllowedParent
- modeStack: scanner state — which bracket group's mode is currently active (code mode at top)
- Built-in language configs: LangGo, LangC, LangJava, LangJS, LangLisp, LangNginx, LangPascal, LangShell
- langRegistry: map[string]BracketLang for CLI lookup by name

## Does
- BracketChunker(lang) Chunker: factory returning a Chunker (also implements FileChunker, RandomAccessChunker, and AppendAwareChunker) for the given language config
- Chunks(path, content, yield): tokenize content with mode-aware scanning, identify groups and paragraphs, yield chunks
- FileChunks(path, old, yield): read file, hash, skip-if-match-old, otherwise delegate to Chunks — delegates to shared `fileChunksByRead` helper (R633, R636)
- ChunkText(path, content, rangeLabel): scan to target range and return its content
- AppendChunks(path, lastLocator, newBytes, yield): delegates to shared `appendByRechunkResume(path, lastLocator, newBytes, bc.Chunks, yield)` helper (R633) — when lastLocator is empty, chunks newBytes alone with replacedLast=false (R629); otherwise decodes the previous last chunk's byte range, reads the file, re-chunks from the previous start through EOF (R627), and adjusts each yielded chunk's Range and Locator to be absolute to the full file (R630). The first emitted chunk decides replacedLast: same byte range as the previous → drop and continue (replacedLast=false); different range → emit as replacement (replacedLast=true) (R628)
- tokenize(content, lang): scan content into a token stream using a mode stack:
  - **code mode** (current group's AllowedInner is nil): comments, all eligible bracket openers, whitespace, text — full scanning
  - **restricted mode** (current group's AllowedInner is non-nil): only this group's Close markers, Escape sequence, and openers listed in AllowedInner are recognized; everything else accumulates as text
  - AllowedParent on a bracket: that bracket is only recognized when the current mode-stack top is one of the listed parent openers
- findGroups(tokens): line-oriented — track depth across code-mode brackets only (string content never affects chunk depth); group starts at first open-bracket line, ends when depth returns to 0
- attachLeading(groups, tokens): attach comment/text lines immediately before a group (no blank line gap)

## Collaborators
- Chunker, FileChunker, RandomAccessChunker, AppendAwareChunker interfaces (implements them)
- fileChunksByRead, appendByRechunkResume (shared helpers in chunker.go)

## Sequences
- seq-bracket-chunk.md
