# Sequence: Indent Chunking

**Requirements:** R326, R327, R328, R329, R330, R331, R332

Shows how IndentChunker.Chunks processes content into groups and paragraphs using indentation levels.

## Participants
- Caller
- IndentChunker (ic)

## Flow

```
Caller                          ic
  |                              |
  |--- Chunks(path,content,yield) -->|
  |                              |
  |                     scan lines, skipping comment/string interiors:
  |                       for each line:
  |                         if inside block comment or multi-line string → continue
  |                         measureIndent(line, tabWidth) → column count
  |                              |
  |                     build scope tree from indentation:
  |                       prevIndent = 0
  |                       stack = [(indent=0, startLine=1)]
  |                       for each non-blank line:
  |                         indent = measureIndent(line)
  |                         if indent > prevIndent → push scope (group start = this line)
  |                         if indent < prevIndent → pop scopes until stack.top.indent <= indent
  |                           each pop marks group end = previous line
  |                         prevIndent = indent
  |                       flush remaining stack entries at EOF
  |                              |
  |                     attachLeading:
  |                       same rule as bracket chunker — comment lines
  |                       immediately before a group header attach to it
  |                              |
  |                     emit chunks:
  |                       walk lines, group spans vs paragraph spans
  |                       lines inside a group → group chunk
  |                       lines outside groups → paragraph chunk
  |                         blank line or group start → flush paragraph
  |                       yield(Chunk{Range: "start-end", Content: bytes})
  |                              |
  |<-- yield(chunk) -------------|
  |                              |
  |<-- nil (done) ---------------|
```
