# Test Design: CLI
**Requirements:** R711
**Source:** crc-CLI.md

## Test: search refuses -verify with -loose or -fuzzy
**Purpose:** -verify checks an exact match, so combining it with a mode that claims something else is refused rather than run
**Input:** searchFlagConflict over every combination of verify, loose, fuzzy
**Expected:** an error naming -verify and the other flag exactly when verify is set together with loose or fuzzy; nil otherwise
**Refs:** crc-CLI.md, R711
**Code:** cmd/microfts/main_test.go
**Fire alarm:** make searchFlagConflict ignore fuzzy (check verify && loose only); red is verify+fuzzy returning nil
**Inject:** cmd/microfts/main.go:searchFlagConflict
**Pulled:** 2026-10-24 — rang: verify+fuzzy returned nil
