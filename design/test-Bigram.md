# Test Design: Bigram
**Source:** crc-Bigram.md

## Test: BigramCounts basic extraction
**Purpose:** Verify bigram extraction with word-boundary padding
**Input:** "cat" with case-insensitive=false, no aliases
**Expected:** `{_c:1, ca:1, at:1, t_:1}` — 4 bigrams with padding
**Refs:** crc-Bigram.md

## Test: BigramCounts multi-word
**Purpose:** Verify word boundary handling across multiple tokens
**Input:** "hello world"
**Expected:** `{_h:1, he:1, el:1, ll:1, lo:1, o_:1, _w:1, wo:1, or:1, rl:1, ld:1, d_:1}` — each word padded independently
**Refs:** crc-Bigram.md

## Test: BigramCounts case insensitive
**Purpose:** Verify case folding before bigram extraction
**Input:** "Cat" with case-insensitive=true
**Expected:** Same as lowercase "cat" bigrams
**Refs:** crc-Bigram.md

## Test: BigramCounts character-internal skip
**Purpose:** Verify multibyte character internal bigrams are skipped
**Input:** String with CJK character (3-byte UTF-8)
**Expected:** Bigrams spanning character boundaries preserved, internal bigrams skipped
**Refs:** crc-Bigram.md

## Test: BigramCounts aliases
**Purpose:** Verify byte aliases applied before extraction
**Input:** Content with aliased bytes
**Expected:** Bigrams reflect aliased values
**Refs:** crc-Bigram.md

## Test: BRecord roundtrip
**Purpose:** Verify BRecord marshal/unmarshal
**Input:** BRecord{Bigram: 0x6361, ChunkIDs: [1, 5, 42]}
**Expected:** Unmarshal produces identical struct
**Refs:** crc-Bigram.md

## Test: CRecord with bigrams roundtrip
**Purpose:** Verify CRecord marshal/unmarshal includes bigram section
**Input:** CRecord with Trigrams, Tokens, Bigrams, FileIDs populated
**Expected:** Roundtrip preserves all fields including Bigrams
**Refs:** crc-Bigram.md

## Test: CRecord without bigrams roundtrip
**Purpose:** Verify CRecord marshal/unmarshal when bigrams disabled
**Input:** CRecord with empty Bigrams, bigramsEnabled=false
**Expected:** Marshal omits bigram section, unmarshal produces empty Bigrams
**Refs:** crc-Bigram.md

## Test: AddFile with bigrams
**Purpose:** Verify AddFile creates B records and C record bigram section
**Input:** Add a file to a bigram-enabled DB
**Expected:** B records exist for file's bigrams, C record contains bigram counts
**Refs:** crc-Bigram.md, seq-bigram-add.md

## Test: AddFile without bigrams
**Purpose:** Verify AddFile skips bigram work when disabled
**Input:** Add a file to a DB created with --no-bigrams
**Expected:** No B records, C record has no bigram section
**Refs:** crc-Bigram.md, seq-bigram-add.md

## Test: RemoveFile cleans B records
**Purpose:** Verify orphaned chunk cleanup includes B records
**Input:** Add file, remove file (orphaning chunks)
**Expected:** B records for orphaned chunks' bigrams have chunkid removed
**Refs:** seq-bigram-add.md

## Test: ScoreBigramOverlap scoring
**Purpose:** Verify bigram overlap scoring for fuzzy matches
**Input:** Query "cat", chunks containing "cat" (100%), "cot" (50%), "dog" (low)
**Expected:** Scores reflect bigram overlap ratio
**Refs:** seq-bigram-search.md

## Test: Search with bigram scoring
**Purpose:** End-to-end search using -score bigram
**Input:** DB with several files, search with WithBigramOverlap
**Expected:** Results scored by bigram overlap, single-character substitutions score higher than trigram-only
**Refs:** seq-bigram-search.md

## Test: Overlay with bigrams
**Purpose:** Verify tmp:// documents include bigram data
**Input:** AddTmpFile on a bigram-enabled DB
**Expected:** Overlay chunks have bigram counts, search with bigram scoring includes overlay results
**Refs:** crc-Overlay.md
