# Trigram Representation

- raw byte trigrams -- every byte is its own value, no character set mapping
  - whitespace bytes (space, tab, newline, carriage return) are word boundaries; runs collapse
  - all non-whitespace bytes are indexed
  - case insensitivity: bytes.ToLower() on input before trigram extraction
  - byte aliases: map byte→byte before extraction (e.g. newline → `^` for line-start matching). Both source and target bytes must be ASCII (< 0x80) — aliasing UTF-8 continuation or leading bytes would corrupt multibyte characters and break character-internal trigram skipping.
  - UTF-8 required — AddFile checks each chunk's Content for valid UTF-8 (utf8.Valid). The raw file itself may be binary (e.g. ODF zip); the chunker is responsible for producing UTF-8 text content.
  - character-internal byte trigrams are skipped during extraction
    - a 3-byte window that falls entirely within a single multibyte character is not emitted
    - 3-byte characters (CJK): 1 internal trigram skipped per character
    - 4-byte characters (emoji): 2 internal trigrams skipped per character
    - 2-byte characters: no internal trigrams possible, no skipping needed
    - ASCII: no multibyte characters, identical behavior
    - cross-boundary trigrams preserved — effectively encode character bigrams for CJK search
  - 8 bits / byte, 24 bits per trigram
  - 16M possible trigrams (2^24 = 16,777,216)
  - trigram counts (C records): sparse individual index records, one per non-zero trigram
