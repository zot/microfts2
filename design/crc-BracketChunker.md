# BracketChunker
**Requirements:** R307, R308, R309, R310, R311, R312, R313, R314, R315, R316, R317, R318, R319, R320, R321, R322, R323, R324

Configurable chunker that groups program text into chunks based on bracket structure. Table-driven — one BracketLang config per language. Implements the Chunker interface.

## Knows
- BracketLang: language-specific lexical rules (comments, strings, brackets)
- StringDelim: open/close/escape for one string delimiter type
- BracketGroup: open/separator/close sets for one bracket type
- Built-in language configs: LangGo, LangC, LangJava, LangJS, LangLisp, LangNginx, LangPascal, LangShell
- langRegistry: map[string]BracketLang for CLI lookup by name

## Does
- BracketChunker(lang) Chunker: factory returning a Chunker for the given language config
- Chunks(path, content, yield): tokenize content, identify groups and paragraphs, yield chunks with startline-endline ranges
- ChunkText(path, content, rangeLabel): scan to target range and return its content
- tokenize(content, lang): scan content into token stream (comment, string, whitespace, bracket, text)
- findGroups(tokens): line-oriented — track unified depth across all bracket types, group starts at first open bracket line, ends when depth returns to 0; single-line groups filtered out
- attachLeading(groups, tokens): attach comment/text lines immediately before a group (no blank line gap)

## Collaborators
- Chunker interface (implements it)

## Sequences
- seq-bracket-chunk.md
