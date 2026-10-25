# MigrateV3
**Requirements:** R722, R723, R724, R725

One-shot command `cmd/microfts2-migrate-v3` that brings a format-"2" database to format "3" by giving every F record a zero `chunkerVersion`. Separate from the library on purpose: `Open` refuses an old format and never migrates. Expected to be deleted once ark's databases are migrated.

## Knows
- dbPath: `-db` flag, required
- bucket: `-bucket` flag, default "fts"

## Does
- main: parse flags, open the bbolt file, run migrate, print the outcome; exit non-zero on error (R722, R725)
- migrate(db, bucket) (rewritten int, already bool, err): one Update txn. Read the `I`+"version" record: "3" → already, change nothing (R724); anything but "2" → refuse. Otherwise rewrite each `F` record with spliceVersion, then set version "3" (R723)
- spliceVersion(value) ([]byte, error): skip modTime (8), contentHash (32), the fileLength varint and the strategy string, insert one 0x00 byte (varint 0); refuse a value too short to hold that header (R723)

## Collaborators
- bbolt: direct access — the library would refuse the old format

## Sequences
- none (single transaction)
