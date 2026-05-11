# Sequence: Bracket Chunking

**Requirements:** R310, R311, R316, R317, R318, R319, R617, R618, R619, R620, R621

Shows how BracketChunker.Chunks processes content into groups and paragraphs, including mode-aware scanning for scan-restricted bracket groups (strings, raw strings, JS template literals).

## Participants
- Caller
- BracketChunker (bc)

## Flow

```
Caller                          bc
  |                              |
  |--- Chunks(path,content,yield) -->|
  |                              |
  |                     tokenize(content, lang) — mode-aware:
  |                       modeStack = [codeMode]    # implicit top-level
  |                       for each byte position:
  |                         if modeStack.top is restricted (AllowedInner != nil):
  |                           try the group's Close markers   → pop modeStack, emit close-bracket token
  |                           try the group's Escape sequence → consume escape + next byte as text
  |                           try openers in AllowedInner     → push that group's mode, emit open-bracket token
  |                           otherwise                       → accumulate one byte into text token
  |                         else (code mode):
  |                           try line comment starts         → skip to EOL, emit comment token
  |                           try block comment starts        → skip to closer, emit comment token
  |                           try every bracket opener whose AllowedParent permits current top:
  |                                                            → emit open-bracket token; if that group has
  |                                                              AllowedInner != nil, push its restricted mode
  |                           try the current group's Close   → emit close-bracket token (decrement depth)
  |                           try bracket separators          → emit separator token
  |                           whitespace run                  → emit whitespace token
  |                           otherwise                       → accumulate text token
  |                              |
  |                     walk token stream, build line index (token → line number)
  |                              |
  |                     findGroups (line-oriented, code-mode brackets only):
  |                       depth = 0, per-line tracking
  |                       for each token:
  |                         open bracket recognized in code mode  → if depth==0: mark line as group start; depth++
  |                         close bracket of code-mode group      → depth--
  |                         (open/close from restricted mode are ignored — string content)
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

## Worked example: JavaScript template literal

Input:

```
let s = `hello ${name} world`;
```

Mode stack and tokens (code = top-level code mode, ` = inside backquote restricted mode, ${ = inside interpolation code mode):

```
position    text                stack-top      action
--------    --------------      ----------     -------------------------
0           let s = `           code           text/whitespace, then bracket-open `
9           hello               `              accumulate as text (no scanning)
15          ${                  `              recognized (in AllowedInner of `); push ${ mode
17          name                ${             code-mode scanning resumes — text "name"
21          }                   ${             close ${; pop back to `
22          world               `              accumulate as text
27          `                   `              close `; pop to code
28          ;                   code           text
```

The whole construct is *not* a chunk group: every bracket open/close happens inside a single line, and the depth in code mode stays at zero (the `${`/`}` pair is in restricted-mode scope of the backtick).
