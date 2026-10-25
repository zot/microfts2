# Test Design: MigrateV3
**Requirements:** R722, R723, R724, R725
**Source:** crc-MigrateV3.md

## Test: v2 database migrates and opens
**Purpose:** a format-"2" database becomes a format-"3" one that the library reads correctly, with chunker version 0 everywhere
**Input:** Create a DB, AddFile two files (one with a long strategy name), then downgrade it to format "2" by removing the chunkerVersion byte from each F record and writing version "2"; run migrate
**Expected:** reports 2 rewritten; Open succeeds; Version() is "3"; each file's FileInfoByID matches what it was before the downgrade (names, strategy, chunks, tokens) with ChunkerVersion 0; CheckFile is fresh
**Refs:** crc-MigrateV3.md, R723, R725
**Code:** cmd/microfts2-migrate-v3/main_test.go
**Fire alarm:** splice the zero byte after the fileLength varint instead of after the strategy; red is Open/FileInfoByID decoding a wrong strategy or names
**Inject:** cmd/microfts2-migrate-v3/main.go:spliceVersion
**Pulled:** 2026-10-25 — rang: FileInfoByID after migrate failed, FRecord: bad name

## Test: second run changes nothing
**Purpose:** running the program on an already-migrated database is harmless
**Input:** migrate the v2 fixture, then run migrate again
**Expected:** second run reports already-current, 0 rewritten, no error; F records byte-identical to after the first run
**Refs:** crc-MigrateV3.md, R724
**Code:** cmd/microfts2-migrate-v3/main_test.go
**Fire alarm:** drop the "3" early return so a second run splices again; red is a second byte inserted and an error or changed bytes
**Inject:** cmd/microfts2-migrate-v3/main.go:migrate
**Pulled:** 2026-10-25 — rang: second migrate = (2, false, nil), want (0, true, nil)

## Test: unknown version refused, untouched
**Purpose:** only format "2" is migrated; anything else leaves the database exactly as it was
**Input:** a database whose version record reads "1"
**Expected:** migrate returns an error naming "1"; every key and value unchanged
**Refs:** crc-MigrateV3.md, R723
**Code:** cmd/microfts2-migrate-v3/main_test.go
**Fire alarm:** remove the "2" check; red is no error
**Inject:** cmd/microfts2-migrate-v3/main.go:migrate
**Pulled:** 2026-10-25 — rang: migrate on version 1 returned nil error
