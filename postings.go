package microfts2

// Reading and writing the T postings under format "6": each posting carries its
// chunk's count of the trigram, a removed chunk's postings stay until a rewrite
// or Purge drops them, and every reader skips the dead set
// (specs/migrations/complete/006-postings-carry-counts.md).

import (
	"bytes"
	"fmt"
	"slices"
	"sort"

	"go.etcd.io/bbolt"
)

// purgeBatchKeys bounds how many T records one Purge write transaction rewrites.
const purgeBatchKeys = 2000

// loadDeadSet reads the D records: the chunkids removed but possibly still in a
// posting list.
// CRC: crc-DB.md | R774, R775
func loadDeadSet(th TxnHolder) map[uint64]struct{} {
	dead := map[uint64]struct{}{}
	c := th.Bucket().Cursor()
	for k, _ := c.Seek([]byte{prefixD}); k != nil && k[0] == prefixD; k, _ = c.Next() {
		if id, n := readUvarint(k[1:]); n > 0 {
			dead[id] = struct{}{}
		}
	}
	return dead
}

// postingReader reads T records inside one transaction: each trigram's postings
// are decoded once, with dead chunks already skipped.
// CRC: crc-DB.md | R775, R776
type postingReader struct {
	th    TxnHolder
	dead  map[uint64]struct{}
	cache map[uint32][]Posting
}

func newPostingReader(th TxnHolder) *postingReader {
	return &postingReader{th: th, cache: map[uint32][]Posting{}}
}

// live returns tri's postings in chunkid order, dead chunks left out.
func (r *postingReader) live(tri uint32) []Posting {
	if ps, ok := r.cache[tri]; ok {
		return ps
	}
	if r.dead == nil {
		r.dead = loadDeadSet(r.th)
	}
	var out []Posting
	if val := r.th.Bucket().Get(makeTKey(tri)); val != nil {
		ps, _ := UnmarshalTValue(val)
		out = ps[:0]
		for _, p := range ps {
			if _, gone := r.dead[p.ChunkID]; !gone {
				out = append(out, p)
			}
		}
	}
	r.cache[tri] = out
	return out
}

// ids is tri's live chunkids as a set.
func (r *postingReader) ids(tri uint32) map[uint64]bool {
	ps := r.live(tri)
	set := make(map[uint64]bool, len(ps))
	for _, p := range ps {
		set[p.ChunkID] = true
	}
	return set
}

// count is how many times tri occurs in chunkid, or 0.
func (r *postingReader) count(tri uint32, chunkid uint64) int {
	ps := r.live(tri)
	i := sort.Search(len(ps), func(i int) bool { return ps[i].ChunkID >= chunkid })
	if i < len(ps) && ps[i].ChunkID == chunkid {
		return ps[i].Count
	}
	return 0
}

// counts is chunkid's count of each of tris that it holds.
// CRC: crc-DB.md | R776, R777
func (r *postingReader) counts(chunkid uint64, tris []uint32) map[uint32]int {
	m := make(map[uint32]int, len(tris))
	for _, t := range tris {
		if c := r.count(t, chunkid); c > 0 {
			m[t] = c
		}
	}
	return m
}

// postingEdits gathers the postings one write operation adds, keyed by trigram,
// so applyPostings rewrites each affected T record once however many chunks
// touch it.
// CRC: crc-DB.md | R784
type postingEdits map[uint32][]Posting

// addChunk queues a new chunk's posting, with its count, on each of its trigrams.
func (pe postingEdits) addChunk(nc newChunkTW) {
	for tri, cnt := range nc.triCounts {
		pe[tri] = append(pe[tri], Posting{ChunkID: nc.chunkid, Count: cnt})
	}
}

// applyPostings rewrites each T record in pe once: its dead postings dropped
// (this operation's orphans among them, since their D records are already
// written in this transaction), the new postings appended, and a record with
// no live posting left deleted.
// CRC: crc-DB.md | R778, R784
func (db *DB) applyPostings(th TxnHolder, pe postingEdits) error {
	if len(pe) == 0 {
		return nil
	}
	b := th.Bucket()
	dead := loadDeadSet(th)
	for tri, add := range pe {
		key := makeTKey(tri)
		stored, err := UnmarshalTValue(b.Get(key))
		if err != nil {
			return fmt.Errorf("T %06x: %w", tri, err)
		}
		if err := db.putPostings(b, key, rewritePostings(stored, dead, add)); err != nil {
			return err
		}
	}
	return nil
}

// putPostings writes one T record value, or deletes the record when val is empty.
func (db *DB) putPostings(b *bbolt.Bucket, key, val []byte) error {
	if db.onPostingWrite != nil {
		db.onPostingWrite(key)
	}
	if len(val) == 0 {
		return b.Delete(key)
	}
	return b.Put(key, val)
}

// rewritePostings encodes stored without its dead postings, then add after the
// survivors. Chunkids come from a counter, so new postings are above every
// stored one; if that ever fails the whole list is put back in chunkid order,
// since the encoding is delta-coded and needs it.
// CRC: crc-DB.md | R770, R778
func rewritePostings(stored []Posting, dead map[uint64]struct{}, add []Posting) []byte {
	out := make([]Posting, 0, len(stored)+len(add))
	for _, p := range stored {
		if _, gone := dead[p.ChunkID]; !gone {
			out = append(out, p)
		}
	}
	out = append(out, add...)
	if !slices.IsSortedFunc(out, func(a, b Posting) int { return cmpChunkID(a.ChunkID, b.ChunkID) }) {
		slices.SortStableFunc(out, func(a, b Posting) int { return cmpChunkID(a.ChunkID, b.ChunkID) })
	}
	return appendPostings(nil, 0, out)
}

func cmpChunkID(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// PurgeStats is what Purge did.
// CRC: crc-DB.md | R779
type PurgeStats struct {
	Lists    int // T records rewritten, deleted ones included
	Postings int // dead postings dropped
	Dead     int // dead-set entries cleared
}

// Purge drops dead postings from every T record. It takes the dead set as it
// stands when it starts, rewrites each T record holding one of those chunkids
// without it (deleting a record left empty), a batch of records per write
// transaction, then deletes those D records. A chunk that dies while it runs
// stays in the dead set for the next purge. With an empty dead set it writes
// nothing.
// CRC: crc-DB.md | R779
func (db *DB) Purge() (PurgeStats, error) {
	var st PurgeStats
	var dead map[uint64]struct{}
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(db.dbName))
		if b == nil {
			return fmt.Errorf("bucket %q not found", db.dbName)
		}
		dead = loadDeadSet(bucketWrap{b})
		return nil
	})
	if err != nil || len(dead) == 0 {
		return st, err
	}
	next := []byte{prefixT}
	for next != nil {
		err := db.bolt.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(db.dbName))
			c := b.Cursor()
			type rewrite struct{ key, val []byte }
			var work []rewrite
			k, v := c.Seek(next)
			for n := 0; k != nil && k[0] == prefixT && n < purgeBatchKeys; k, v = c.Next() {
				n++
				ps, err := UnmarshalTValue(v)
				if err != nil {
					return fmt.Errorf("T %x: %w", k, err)
				}
				kept := ps[:0:0]
				for _, p := range ps {
					if _, gone := dead[p.ChunkID]; gone {
						st.Postings++
					} else {
						kept = append(kept, p)
					}
				}
				if len(kept) < len(ps) {
					work = append(work, rewrite{bytes.Clone(k), appendPostings(nil, 0, kept)})
				}
			}
			if k != nil && k[0] == prefixT {
				next = bytes.Clone(k)
			} else {
				next = nil
			}
			for _, w := range work {
				if err := db.putPostings(b, w.key, w.val); err != nil {
					return err
				}
			}
			st.Lists += len(work)
			return nil
		})
		if err != nil {
			return st, err
		}
	}
	err = db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(db.dbName))
		for id := range dead {
			if err := b.Delete(makeDKey(id)); err != nil {
				return err
			}
		}
		st.Dead = len(dead)
		return nil
	})
	return st, err
}
