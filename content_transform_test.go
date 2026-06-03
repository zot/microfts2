package microfts2

// CRC: crc-Chunker.md, crc-DB.md, crc-ChunkCache.md, crc-Overlay.md | test-ContentTransform.md | R642, R643, R644, R645, R646, R647, R649, R650, R655, R656, R657, R658, R659
// Tests for the per-chunker content transform. See design/test-ContentTransform.md.
// The transform is index-only: it shapes the trigram/token index and the derived
// Attrs, while retrieval returns the original chunker content.

import (
	"bytes"
	"fmt"
	"testing"
)

// stripTags is a test transform standing in for ark's tag elision: it removes
// lines of the form "@key: value" from Content and appends each as an Attr.
func stripTags(c *Chunk) {
	lines := bytes.Split(c.Content, []byte("\n"))
	kept := lines[:0:0]
	for _, ln := range lines {
		t := bytes.TrimSpace(ln)
		if bytes.HasPrefix(t, []byte("@")) {
			if i := bytes.IndexByte(t, ':'); i > 1 {
				key := bytes.TrimSpace(t[1:i])
				val := bytes.TrimSpace(t[i+1:])
				c.Attrs = append(c.Attrs, Pair{
					Key:   append([]byte(nil), key...),
					Value: append([]byte(nil), val...),
				})
				continue
			}
		}
		kept = append(kept, ln)
	}
	c.Content = bytes.Join(kept, []byte("\n"))
}

// paraChunker splits content into blank-line-separated paragraphs. It implements
// only Chunker (not RandomAccessChunker), so retrieval exercises the streaming
// fallback.
type paraChunker struct{}

func (paraChunker) Chunks(_ string, content []byte, yield func(Chunk) bool) error {
	n := 0
	for _, p := range bytes.Split(content, []byte("\n\n")) {
		if len(bytes.TrimSpace(p)) == 0 {
			continue
		}
		n++
		if !yield(Chunk{Range: []byte(fmt.Sprintf("p%d", n)), Content: append([]byte(nil), p...)}) {
			return nil
		}
	}
	return nil
}

// R646, R647, R656: the transform's stripped Content feeds the trigram index, but
// the C record stores the original Content (hashed over the original).
func TestContentTransformStripsTheIndexNotTheStore(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para", paraChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.txt", "@author: zorblax\nthe quick maluba")
	fileid, err := db.AddFile(f, "para")
	if err != nil {
		t.Fatal(err)
	}

	// Body text is indexed.
	if got := mustSearchCount(t, db, "maluba"); got == 0 {
		t.Errorf("search %q: expected a hit, got none", "maluba")
	}
	// Stripped tag value is NOT indexed.
	if got := mustSearchCount(t, db, "zorblax"); got != 0 {
		t.Errorf("search %q: expected no hits (tag stripped from index), got %d", "zorblax", got)
	}

	// Stored content is the ORIGINAL (tags intact); Attrs carry the derived value.
	chunk := onlyChunk(t, db, fileid, f)
	if chunk.Content != "@author: zorblax\nthe quick maluba" {
		t.Errorf("stored Content = %q, want the original tag-bearing text", chunk.Content)
	}
	if v, ok := PairGet(chunk.Attrs, "author"); !ok || string(v) != "zorblax" {
		t.Errorf("indexed Attrs author = %q,%v, want zorblax", v, ok)
	}
}

// R655: streaming-fallback retrieval (non-RandomAccessChunker) returns the
// original chunker Content; Attrs come from the stored C record.
func TestContentTransformRetrieveStreaming(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para", paraChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.txt", "@author: zorblax\nthe quick maluba")
	fileid, err := db.AddFile(f, "para")
	if err != nil {
		t.Fatal(err)
	}

	chunk := onlyChunk(t, db, fileid, f)
	if chunk.Content != "@author: zorblax\nthe quick maluba" {
		t.Errorf("retrieved Content = %q, want the original tag-bearing text", chunk.Content)
	}
	if v, ok := PairGet(chunk.Attrs, "author"); !ok || string(v) != "zorblax" {
		t.Errorf("retrieved Attrs author = %q,%v, want zorblax", v, ok)
	}
}

// R655: RandomAccessChunker fast-path retrieval returns the original Content and
// pre-fills Attrs from the stored C record — no transform, no double-append.
func TestContentTransformRetrieveFastPath(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("md-strip", MarkdownChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.md", "@author: zorblax\nthe quick maluba\n")
	fileid, err := db.AddFile(f, "md-strip")
	if err != nil {
		t.Fatal(err)
	}

	chunk := onlyChunk(t, db, fileid, f)
	if !bytes.Contains([]byte(chunk.Content), []byte("the quick maluba")) {
		t.Errorf("fast-path Content = %q, want it to contain %q", chunk.Content, "the quick maluba")
	}
	// Retrieval returns the ORIGINAL content — the tag text is present, not stripped.
	if !bytes.Contains([]byte(chunk.Content), []byte("zorblax")) {
		t.Errorf("fast-path Content = %q, want it to contain the original tag text %q", chunk.Content, "zorblax")
	}
	// Attr present exactly once (from the C record, not double-appended).
	count := 0
	for _, p := range chunk.Attrs {
		if string(p.Key) == "author" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("author attr appears %d times on fast path, want exactly 1", count)
	}
}

// R655, R656: a chunk that is entirely @tag lines indexes to empty body text, yet
// retrieves as its original (non-empty) tag text. This is the regression guard:
// before the index-only fix, retrieval pre-stripped and handed back empty content.
func TestContentTransformAllTagChunkRetrievesOriginal(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para", paraChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "req.txt", "@from: ark\n@to: microfts2")
	fileid, err := db.AddFile(f, "para")
	if err != nil {
		t.Fatal(err)
	}

	// Tag values are not in the trigram index.
	if got := mustSearchCount(t, db, "microfts2"); got != 0 {
		t.Errorf("search %q: expected no hits (all-tag body stripped from index), got %d", "microfts2", got)
	}

	// Retrieval hands back the original tag text — never empty.
	chunk := onlyChunk(t, db, fileid, f)
	if chunk.Content != "@from: ark\n@to: microfts2" {
		t.Errorf("retrieved Content = %q, want the original all-tag text (not empty)", chunk.Content)
	}
	if v, ok := PairGet(chunk.Attrs, "from"); !ok || string(v) != "ark" {
		t.Errorf("Attrs from = %q,%v, want ark", v, ok)
	}
	if v, ok := PairGet(chunk.Attrs, "to"); !ok || string(v) != "microfts2" {
		t.Errorf("Attrs to = %q,%v, want microfts2", v, ok)
	}
}

// R658: an append that adds a tag to the trailing paragraph changes that chunk's
// ORIGINAL content, so it hashes to a fresh chunkid and WithIndexedChunkCallback
// fires on the append path — natural re-index, no Attrs-in-hash workaround.
func TestContentTransformAppendNewChunkID(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("md-strip", MarkdownChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.md", "@author: zorblax\nmaluba body\n")
	fileid, err := db.AddFile(f, "md-strip")
	if err != nil {
		t.Fatal(err)
	}
	origID := onlyChunkID(t, db, fileid)

	// Append a tag line to the (only) paragraph. The full file on disk must hold
	// the new content; AppendChunks receives only the appended bytes.
	writeTestFile(t, dir, "doc.md", "@author: zorblax\nmaluba body\n@extra: nine\n")
	var fired []IndexedChunk
	if err := db.AppendChunks(fileid, []byte("@extra: nine\n"), "md-strip",
		WithIndexedChunkCallback(func(ic IndexedChunk) { fired = append(fired, ic) })); err != nil {
		t.Fatal(err)
	}

	newID := onlyChunkID(t, db, fileid)
	if newID == origID {
		t.Errorf("chunkid unchanged (%d) after a tag-adding append; expected a fresh chunkid", newID)
	}
	if len(fired) == 0 {
		t.Fatal("WithIndexedChunkCallback did not fire on append")
	}
	last := fired[len(fired)-1]
	if v, ok := PairGet(last.CRecord.Attrs, "extra"); !ok || string(v) != "nine" {
		t.Errorf("appended chunk Attrs extra = %q,%v, want nine", v, ok)
	}
	if _, ok := PairGet(last.CRecord.Attrs, "author"); !ok {
		t.Errorf("appended chunk lost the original author attr: %v", last.CRecord.Attrs)
	}
}

// R656, R657: dedup identity is the original content. Files whose tags differ have
// different original content and get distinct chunkids; files with identical
// original content (tags and all) dedup to one chunkid.
func TestContentTransformIdentityIsOriginalContent(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para", paraChunker{}, stripTags); err != nil {
		t.Fatal(err)
	}

	// Differing tags → differing original content → distinct chunkids.
	fx := writeTestFile(t, dir, "x.txt", "@a: 1\nshared body text")
	fy := writeTestFile(t, dir, "y.txt", "@b: 2\nshared body text")
	idx, err := db.AddFile(fx, "para")
	if err != nil {
		t.Fatal(err)
	}
	idy, err := db.AddFile(fy, "para")
	if err != nil {
		t.Fatal(err)
	}
	if onlyChunkID(t, db, idx) == onlyChunkID(t, db, idy) {
		t.Error("chunks whose tags differ share a chunkid; expected distinct chunkids (different original content)")
	}

	// Identical original content (same tags) → dedup to one chunkid.
	fp := writeTestFile(t, dir, "p.txt", "@a: 1\nshared body text")
	fq := writeTestFile(t, dir, "q.txt", "@a: 1\nshared body text")
	idp, err := db.AddFile(fp, "para")
	if err != nil {
		t.Fatal(err)
	}
	idq, err := db.AddFile(fq, "para")
	if err != nil {
		t.Fatal(err)
	}
	if onlyChunkID(t, db, idp) != onlyChunkID(t, db, idq) {
		t.Error("identical original content did not dedup to one chunkid")
	}
}

// R656: a chunk produced without a transform hashes as SHA-256 over Content, so
// identical content still deduplicates exactly as before.
func TestContentTransformNoTransformStillDedup(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para-plain", paraChunker{}, nil); err != nil {
		t.Fatal(err)
	}
	fx := writeTestFile(t, dir, "x.txt", "shared body text")
	fy := writeTestFile(t, dir, "y.txt", "shared body text")
	idx, err := db.AddFile(fx, "para-plain")
	if err != nil {
		t.Fatal(err)
	}
	idy, err := db.AddFile(fy, "para-plain")
	if err != nil {
		t.Fatal(err)
	}
	if onlyChunkID(t, db, idx) != onlyChunkID(t, db, idy) {
		t.Error("identical no-transform content did not deduplicate to one chunkid")
	}
}

// R643, R644, R645: a nil transform (and AddStrategyFunc) leaves content untouched.
func TestContentTransformNilPassthrough(t *testing.T) {
	db, dir := testDB(t)
	if err := db.AddChunker("para-plain", paraChunker{}, nil); err != nil {
		t.Fatal(err)
	}
	f := writeTestFile(t, dir, "doc.txt", "@author: zorblax\nthe quick maluba")
	fileid, err := db.AddFile(f, "para-plain")
	if err != nil {
		t.Fatal(err)
	}
	chunk := onlyChunk(t, db, fileid, f)
	if !bytes.Contains([]byte(chunk.Content), []byte("@author: zorblax")) {
		t.Errorf("nil transform should pass content through untouched, got %q", chunk.Content)
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
