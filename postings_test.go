package microfts2

// CRC: crc-DB.md | Test: test-DB.md | R770-R779

import (
	"testing"

	"go.etcd.io/bbolt"
)

// deadIDs lists the chunkids with D records.
func deadIDs(t *testing.T, db *DB) map[uint64]struct{} {
	t.Helper()
	var dead map[uint64]struct{}
	db.bolt.View(func(tx *bbolt.Tx) error {
		dead = loadDeadSet(bucketWrap{tx.Bucket([]byte(db.dbName))})
		return nil
	})
	return dead
}

func chunkIDsOf(t *testing.T, db *DB, path string) []uint64 {
	t.Helper()
	var ids []uint64
	db.bolt.View(func(tx *bbolt.Tx) error {
		th := bucketWrap{tx.Bucket([]byte(db.dbName))}
		_, f, err := db.lookupFileByPath(th, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range f.Chunks {
			ids = append(ids, c.ChunkID)
		}
		return nil
	})
	return ids
}

// TestRewriteDropsDeadPostings: an add that rewrites a T record drops the dead
// postings in it, and leaves the D record (other lists may hold the id).
// Test: test-DB.md | CRC: crc-DB.md | R778
func TestRewriteDropsDeadPostings(t *testing.T) {
	db, dir := testDB(t)
	a := writeTestFile(t, dir, "a.txt", "common alpha\n")
	if _, err := db.AddFile(a, "line"); err != nil {
		t.Fatal(err)
	}
	gone := chunkIDsOf(t, db, a)[0]
	if err := db.RemoveFile(a); err != nil {
		t.Fatal(err)
	}
	b := writeTestFile(t, dir, "b.txt", "common beta\n")
	if _, err := db.AddFile(b, "line"); err != nil {
		t.Fatal(err)
	}
	db.bolt.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(db.dbName))
		ps, _ := UnmarshalTValue(bk.Get(makeTKey(db.trigrams.ExtractTrigrams([]byte("com"))[0])))
		for _, p := range ps {
			if p.ChunkID == gone {
				t.Errorf("the rewritten \"com\" record still stores dead chunk %d: %v", gone, ps)
			}
		}
		if len(ps) != 1 {
			t.Errorf("\"com\" postings: got %v, want only the new chunk", ps)
		}
		if !isDeadInBucket(bk, gone) {
			t.Errorf("the D record for %d went with the rewrite", gone)
		}
		return nil
	})
}

// TestPurge: Purge drops every dead posting, deletes records left with none,
// clears the dead set it started with and reports what it did; a second Purge
// writes nothing.
// Test: test-DB.md | CRC: crc-DB.md | R779
func TestPurge(t *testing.T) {
	db, dir := testDB(t)
	a := writeTestFile(t, dir, "a.txt", "common zyxwv\ncommon second\n")
	keep := writeTestFile(t, dir, "keep.txt", "common keeper\n")
	for _, p := range []string{a, keep} {
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
	}
	dead := chunkIDsOf(t, db, a)
	if err := db.RemoveFile(a); err != nil {
		t.Fatal(err)
	}
	st, err := db.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if st.Dead != 2 || st.Postings == 0 || st.Lists == 0 {
		t.Errorf("PurgeStats = %+v, want 2 dead and some postings and lists", st)
	}
	db.bolt.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(db.dbName))
		for _, id := range dead {
			if storedPosting(bk, id) {
				t.Errorf("purged chunk %d is still stored in a posting", id)
			}
		}
		zyx := db.trigrams.ExtractTrigrams([]byte("zyx"))[0]
		if bk.Get(makeTKey(zyx)) != nil {
			t.Error("the T record only the removed file held was not deleted")
		}
		return nil
	})
	if left := deadIDs(t, db); len(left) != 0 {
		t.Errorf("dead set after Purge: %v, want empty", left)
	}
	if sr, _ := db.Search("common"); len(sr.Results) != 1 || sr.Results[0].Path != keep {
		t.Errorf("common after Purge: %v, want only %s", sr.Results, keep)
	}
	writes := countPostingWrites(db)
	if st, err := db.Purge(); err != nil || st != (PurgeStats{}) || len(writes) != 0 {
		t.Errorf("second Purge: %+v, %v, writes %v; want nothing", st, err, writes)
	}
}

// TestCountsComeFromPostings: a chunk's count of a trigram reaches the scorers
// from its posting: "aaaaaa xx" holds "aaa" four times and two distinct
// tokens, so density scores 4/2 in Search and in ScoreFile alike.
// Test: test-DB.md | CRC: crc-DB.md | R776, R777
func TestCountsComeFromPostings(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "a.txt", "aaaaaa xx\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	sr, err := db.Search("aaa", WithDensity())
	if err != nil || len(sr.Results) != 1 {
		t.Fatalf("Search: %v, %v", sr, err)
	}
	if got := sr.Results[0].Score; got != 2.0 {
		t.Errorf("Search density score = %v, want 2 (count 4 / 2 distinct tokens)", got)
	}
	sc, err := db.ScoreFile("aaa", fp, ScoreDensityFunc)
	if err != nil || len(sc) != 1 {
		t.Fatalf("ScoreFile: %v, %v", sc, err)
	}
	if got := sc[0].Score; got != 2.0 {
		t.Errorf("ScoreFile density score = %v, want 2", got)
	}
}

// TestOrphanSubtractsTokenTotal: totalTokens rises by a new chunk's token total
// and falls by it when the chunk is orphaned.
// Test: test-DB.md | CRC: crc-DB.md | R774
func TestOrphanSubtractsTokenTotal(t *testing.T) {
	db, dir := testDB(t)
	fp := writeTestFile(t, dir, "a.txt", "aa aa aa bb\n")
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	total := func() uint64 {
		var n uint64
		db.bolt.View(func(tx *bbolt.Tx) error {
			n, _ = iCounter(bucketWrap{tx.Bucket([]byte(db.dbName))}, "totalTokens")
			return nil
		})
		return n
	}
	if got := total(); got != 4 {
		t.Errorf("totalTokens after add = %d, want 4", got)
	}
	if err := db.RemoveFile(fp); err != nil {
		t.Fatal(err)
	}
	if got := total(); got != 0 {
		t.Errorf("totalTokens after remove = %d, want 0", got)
	}
}

// TestLooseCountsEveryTermTrigram: loose search's term scorer checks every
// trigram of a term, the ones the trigram filter dropped included, so their
// counts are taken from the postings too. "zqxy" has "zqx" (rare, kept) and
// "qxy" (common, dropped by FilterBestN(1)); the chunk holding both matches.
// Test: test-DB.md | CRC: crc-DB.md | R776, R340
func TestLooseCountsEveryTermTrigram(t *testing.T) {
	db, dir := testDB(t)
	paths := []string{
		writeTestFile(t, dir, "hit.txt", "zqxy\n"),
		writeTestFile(t, dir, "q1.txt", "qxy one\n"),
		writeTestFile(t, dir, "q2.txt", "qxy two\n"),
		writeTestFile(t, dir, "q3.txt", "qxy three\n"),
	}
	for _, p := range paths {
		if _, err := db.AddFile(p, "line"); err != nil {
			t.Fatal(err)
		}
	}
	sr, err := db.Search("zqxy", WithLoose(), WithTrigramFilter(FilterBestN(1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Results) != 1 || sr.Results[0].Path != paths[0] {
		t.Errorf("loose zqxy: got %v, want %s", sr.Results, paths[0])
	}
}

// TestPurgeAcrossBatches: Purge rewrites T records a batch at a time; with more
// trigram records than one batch holds, every one of them still loses its
// dead postings. Found by injecting past the alarm list: stopping after the
// first batch left the suite green.
// Test: test-DB.md | CRC: crc-DB.md | R779
func TestPurgeAcrossBatches(t *testing.T) {
	db, dir := testDB(t)
	var words []byte
	for a := 'a'; a < 'q'; a++ {
		for b := 'a'; b < 'q'; b++ {
			for c := 'a'; c < 'q'; c++ {
				words = append(words, byte(a), byte(b), byte(c), ' ')
			}
		}
	}
	words = append(words, '\n')
	fp := writeTestFile(t, dir, "many.txt", string(words))
	if _, err := db.AddFile(fp, "line"); err != nil {
		t.Fatal(err)
	}
	countT := func() int {
		n := 0
		db.bolt.View(func(tx *bbolt.Tx) error {
			c := tx.Bucket([]byte(db.dbName)).Cursor()
			for k, _ := c.Seek([]byte{prefixT}); k != nil && k[0] == prefixT; k, _ = c.Next() {
				n++
			}
			return nil
		})
		return n
	}
	if n := countT(); n <= purgeBatchKeys {
		t.Fatalf("fixture has %d T records, need more than one batch (%d)", n, purgeBatchKeys)
	}
	if err := db.RemoveFile(fp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Purge(); err != nil {
		t.Fatal(err)
	}
	if n := countT(); n != 0 {
		t.Errorf("%d T records left after purging the only file's chunk, want 0", n)
	}
}
