package microfts2

// CRC: crc-DB.md, crc-ChunkCache.md, crc-Overlay.md | test-ChunkAttrs.md | R655, R656, R657
// The ContentTransform hook was rolled back (retire-content-transform). These
// tests cover the steady state it left behind: the trigram index is full-text
// (content indexed verbatim, tags and all), dedup is by content hash, and native
// chunker Attrs are stored at index time and surfaced on every retrieval path.

import (
	"testing"
)

// attrChunker yields the whole content as one chunk carrying a native Attr.
// It implements only Chunker (not RandomAccessChunker), so retrieval exercises
// the streaming path — the path that previously dropped stored Attrs.
type attrChunker struct{}

func (attrChunker) Chunks(_ string, content []byte, yield func(Chunk) bool) error {
	yield(Chunk{
		Range:   []byte("all"),
		Content: append([]byte(nil), content...),
		Attrs:   []Pair{{Key: []byte("kind"), Value: []byte("doc")}},
	})
	return nil
}

// R656: the trigram index is full-text — content is indexed verbatim, including
// any @tag: spans. microfts2 strips nothing; this is the point of the rollback.
func TestIndexKeepsTags(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("plain", attrChunker{}); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.txt", "the quick @note: bubba maluba")
	if _, err := db.AddFile(f, "plain"); err != nil {
		t.Fatal(err)
	}
	if got := mustSearchCount(t, db, "bubba"); got == 0 {
		t.Errorf("search %q: full-text index must find tagged text, got 0 hits", "bubba")
	}
	if got := mustSearchCount(t, db, "maluba"); got == 0 {
		t.Errorf("search %q: expected a hit, got 0", "maluba")
	}
}

// R655: a native chunker Attr stored at index time is surfaced on retrieval,
// including the streaming (non-RandomAccessChunker) path that read the chunker's
// re-yielded Attrs before 586a0ae. Content is the original file bytes.
func TestNativeAttrsSurviveStreamingRetrieval(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("attr", attrChunker{}); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.txt", "body text here")
	fileid, err := db.AddFile(f, "attr")
	if err != nil {
		t.Fatal(err)
	}
	chunk := onlyChunk(t, db, fileid, f)
	if chunk.Content != "body text here" {
		t.Errorf("retrieved Content = %q, want %q", chunk.Content, "body text here")
	}
	if v, ok := PairGet(chunk.Attrs, "kind"); !ok || string(v) != "doc" {
		t.Errorf("retrieved Attrs kind = %q,%v, want doc — native Attrs must survive streaming retrieval", v, ok)
	}
}

// R657: dedup identity is the content hash — two files with identical content
// share one chunkid; differing content gets distinct chunkids.
func TestDedupByContentHash(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("attr", attrChunker{}); err != nil {
		t.Fatal(err)
	}
	fx := writeTestFile(t, dir, "x.txt", "shared body")
	fy := writeTestFile(t, dir, "y.txt", "shared body")
	fz := writeTestFile(t, dir, "z.txt", "different body")
	idx, err := db.AddFile(fx, "attr")
	if err != nil {
		t.Fatal(err)
	}
	idy, err := db.AddFile(fy, "attr")
	if err != nil {
		t.Fatal(err)
	}
	idz, err := db.AddFile(fz, "attr")
	if err != nil {
		t.Fatal(err)
	}
	if onlyChunkID(t, db, idx) != onlyChunkID(t, db, idy) {
		t.Error("identical content did not dedup to one chunkid")
	}
	if onlyChunkID(t, db, idx) == onlyChunkID(t, db, idz) {
		t.Error("differing content shared a chunkid; expected distinct")
	}
}

// --- helpers ---

func mustSearchCount(t *testing.T, db *DB, query string) int {
	t.Helper()
	res, err := db.Search(query)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	return len(res.Results)
}

func onlyChunk(t *testing.T, db *DB, fileid uint64, fpath string) ChunkResult {
	t.Helper()
	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatalf("FileInfoByID(%d): %v", fileid, err)
	}
	if len(frec.Chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk in %s, got %d", fpath, len(frec.Chunks))
	}
	chunks, err := db.GetChunks(fpath, frec.Chunks[0].Location, 0, 0)
	if err != nil {
		t.Fatalf("GetChunks(%s): %v", fpath, err)
	}
	if len(chunks) != 1 {
		t.Fatalf("GetChunks(%s) returned %d chunks, want 1", fpath, len(chunks))
	}
	return chunks[0]
}

func onlyChunkID(t *testing.T, db *DB, fileid uint64) uint64 {
	t.Helper()
	frec, err := db.FileInfoByID(fileid)
	if err != nil {
		t.Fatalf("FileInfoByID(%d): %v", fileid, err)
	}
	if len(frec.Chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk in fileid %d, got %d", fileid, len(frec.Chunks))
	}
	return frec.Chunks[0].ChunkID
}
