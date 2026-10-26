# Case-insensitive filenames

On a case-insensitive filesystem (Windows), `Casey.md` and `CASEY.md` name the
same file. microfts2 indexes a file by its path, so without help a rename that
changes only case leaves the file indexed twice, and nothing ever removes the
old entry, because a stat of the old spelling still succeeds. A database can
ask microfts2 to treat paths that differ only in case as one file.

## The option

`Options.CaseInsensitiveFilenames bool` is a creation-time option, like
`CaseInsensitive`. `Create` writes it to the `I` record
`caseInsensitiveFilenames`, and `Open` reads it back. An absent record means
off, so every database created before the option existed behaves exactly as it
did. It is unrelated to `CaseInsensitive`, which governs content trigrams.

## The fold

With the option on, a path's **filename key** is `strings.ToUpper(path)`, over
the whole path, extension included: `C:\t171\src\Casey.md` keys as
`C:\T171\SRC\CASEY.MD`. Uppercase is the comparison Windows itself uses. With
the option off, the filename key is the path unchanged.

```go
// FilenameKey returns the key microfts2 uses to identify a file path:
// the path folded to upper case when the database was created with
// CaseInsensitiveFilenames, otherwise the path unchanged.
func (db *DB) FilenameKey(path string) string
```

`FilenameKey` is the one definition of the fold. A host that keeps its own
path-keyed state beside the index uses it, so its keys and microfts2's agree.

**Every N key is computed from the filename key**, never from the path as
given: the lookup, the duplicate guard, the N records written when a file is
added, and the ones deleted when it is removed. The exported key-chain
functions (`EncodeFilename`, `FinalKey`, `DecodeFilename`) stay pure
encoders of the string they are given; the database folds before calling them.

**The stored spelling is not folded.** The F record's `Names` and the final N
value keep the path as given, so every path microfts2 reports is spelled the
way it was last given (see Respelling below).

**The in-memory path→fileid cache is keyed by the filename key**, so it agrees
with the N records.

**`tmp://` overlay paths do not fold.** They name no file on any filesystem.

## Respelling: the last spelling wins

After `Casey.md` is renamed `CASEY.md`, the folded key still finds the file,
but its stored spelling is `Casey.md`, and that is what microfts2 would report
forever. So **a path-taking write that reaches an existing file through a
spelling different from the stored one updates the stored spelling** to the
one it was given: the F record's `Names`, the final N value, and the in-memory
path cache.

- **`Reindex` respells** as a consequence of what it already does: it removes
  the file's metadata and adds it again under the path it was given.
- **`AddFile` respells, then returns `ErrAlreadyIndexed`.** The error stays
  true, since the file is already indexed, so a caller's handling does not
  change. Only the stored spelling moves; chunks, fileid and staleness data are
  untouched.
- **`AddFile` with the identical spelling writes nothing** and returns
  `ErrAlreadyIndexed`, as it always has.
- **Respelling needs the option.** With it off, two spellings are two files,
  so there is never a different spelling of the same file to respell.

This is the path a host on a case-insensitive filesystem takes. After a
rename, a staleness check stats the stored spelling, which still succeeds, so
nothing looks stale. The host's next scan finds the new spelling and calls
`AddFile`.
