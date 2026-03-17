# Sequence: Bracket Chunking

**Requirements:** R310, R311, R316, R317, R318, R319

Shows how BracketChunker.Chunks processes content into groups and paragraphs.

## Participants
- Caller
- BracketChunker (bc)

## Flow

```
Caller                          bc
  |                              |
  |--- Chunks(path,content,yield) -->|
  |                              |
  |                     tokenize(content, lang)
  |                       for each byte position:
  |                         try line comment starts  → skip to EOL, emit comment token
  |                         try block comment starts → skip to closer, emit comment token
  |                         try string delimiters    → skip to closer (respecting escape), emit string token
  |                         try bracket openers      → emit open-bracket token
  |                         try bracket separators   → emit separator token
  |                         try bracket closers      → emit close-bracket token
  |                         whitespace run           → emit whitespace token
  |                         otherwise                → accumulate text token
  |                              |
  |                     walk token stream, build line index (token → line number)
  |                              |
  |                     findGroups (line-oriented):
  |                       depth = 0, per-line tracking
  |                       for each token:
  |                         open bracket  → if depth==0: mark line as group start; depth++
  |                         close bracket → depth--
  |                         separator     → (stays in current group, depth unchanged)
  |                       per-line depth check:
  |                         depth > 0 at end of line → group continues
  |                         depth == 0 at end of line → group ends at this line
  |                       filter: discard single-line groups (e.g. "f()" on one line)
  |                              |
  |                     attachLeading:
  |                       for each group, scan backward from group start:
  |                         if prev line is comment/text (no blank line gap) → extend group start
  |                         stop at blank line or another group's end
  |                              |
  |                     emit chunks:
  |                       walk content line by line, current position vs group spans
  |                       lines inside a group → accumulate into group chunk
  |                       lines outside groups → accumulate into paragraph chunk
  |                         blank line or group start → flush paragraph
  |                       each flush: yield(Chunk{Range: "start-end", Content: bytes})
  |                              |
  |<-- yield(chunk) -------------|  (per group or paragraph)
  |                              |
  |<-- nil (done) ---------------|
```
