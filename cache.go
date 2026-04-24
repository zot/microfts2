package microfts2

// CRC: crc-ChunkCache.md | Seq: seq-cache.md, seq-chunker-dispatch.md
// R297, R298, R299, R300, R301, R302, R303, R304, R305, R306, R529, R535, R536, R537, R538, R539, R540, R541, R542, R543, R544, R545

import (
	"fmt"
	"os"

	"github.com/bmatsuo/lmdb-go/lmdb"
)

// ChunkCache is a per-query cache for file content and chunked data.
// Avoids redundant file reads and re-chunking when processing search results.
type ChunkCache struct {
	db    *DB
	files map[string]*cachedFile
}

type cachedFile struct {
	path       string
	data       []byte             // nil for FileChunker-only (chunker reads file directly)
	chunker    any                // Chunker, FileChunker, and/or RandomAccessChunker
	fileChunks []FileChunkEntry   // R538: positional chunk list from frec.Chunks
	rangeIds   map[string]uint64  // R539: Location → ChunkID
	chunks     []cachedChunk      // R540: access-order, not positional
	byRange    map[string]int     // Location → index into chunks
	customData any                // R541: per-file scratch for RandomAccessChunker
	complete   bool               // true once streaming path has exhausted the file
}

type cachedChunk struct {
	Range   string
	Content []byte
	Attrs   []Pair
}

// NewChunkCache creates a per-query chunk cache.
func (db *DB) NewChunkCache() *ChunkCache {
	return &ChunkCache{
		db:    db,
		files: make(map[string]*cachedFile),
	}
}

// ChunkText returns a single chunk's content by range label. R536
// Convenience wrapper — resolves chunkID via rangeIds, delegates to ChunkTextWithId.
func (cc *ChunkCache) ChunkText(fpath, rangeLabel string) ([]byte, bool) {
	cf, err := cc.ensureFile(fpath)
	if err != nil {
		return nil, false
	}
	chunkID, ok := cf.rangeIds[rangeLabel]
	if !ok {
		return nil, false
	}
	return cc.resolveChunk(cf, chunkID, rangeLabel)
}

// ChunkTextWithId returns a single chunk's content by ChunkID. R535
// Fast path for callers that already have a chunkID (e.g. SearchResult).
func (cc *ChunkCache) ChunkTextWithId(fpath string, chunkID uint64) ([]byte, bool) {
	cf, err := cc.ensureFile(fpath)
	if err != nil {
		return nil, false
	}
	loc, ok := cf.lookupLocation(chunkID)
	if !ok {
		return nil, false
	}
	return cc.resolveChunk(cf, chunkID, loc)
}

// resolveChunk serves cached content or dispatches to fast/streaming retrieval.
func (cc *ChunkCache) resolveChunk(cf *cachedFile, chunkID uint64, loc string) ([]byte, bool) {
	if idx, ok := cf.byRange[loc]; ok {
		return cf.chunks[idx].Content, true
	}
	if ra, ok := cf.chunker.(RandomAccessChunker); ok {
		if !cc.retrieveFast(cf, chunkID, loc, ra) {
			return nil, false
		}
		return cf.chunks[cf.byRange[loc]].Content, true
	}
	return cc.retrieveStream(cf, loc)
}

// GetChunks retrieves the target chunk and up to before/after positional neighbors. R544
// Same contract as DB.GetChunks but cached.
func (cc *ChunkCache) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error) {
	cf, err := cc.ensureFile(fpath)
	if err != nil {
		return nil, err
	}

	targetPos := -1
	for i, fce := range cf.fileChunks {
		if fce.Location == targetRange {
			targetPos = i
			break
		}
	}
	if targetPos < 0 {
		return nil, fmt.Errorf("range %q not found in %s", targetRange, fpath)
	}

	lo := max(0, targetPos-before)
	hi := min(len(cf.fileChunks)-1, targetPos+after)

	if ra, ok := cf.chunker.(RandomAccessChunker); ok {
		if err := cc.populateFastWindow(cf, ra, lo, hi); err != nil {
			return nil, err
		}
	} else if !cf.complete {
		cc.chunkFull(cf)
	}

	results := make([]ChunkResult, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		fce := cf.fileChunks[i]
		idx, ok := cf.byRange[fce.Location]
		if !ok {
			continue
		}
		ch := cf.chunks[idx]
		results = append(results, ChunkResult{
			Path:    fpath,
			Range:   ch.Range,
			Content: string(ch.Content),
			Index:   i,
			Attrs:   ch.Attrs,
		})
	}
	return results, nil
}

// ensureFile resolves a file path and prepares the cached file entry.
func (cc *ChunkCache) ensureFile(fpath string) (*cachedFile, error) {
	if cf, ok := cc.files[fpath]; ok {
		return cf, nil
	}

	var frec FRecord
	err := cc.db.env.View(func(txn *lmdb.Txn) error {
		_, f, err := cc.db.lookupFileByPath(txnWrap{txn}, fpath)
		if err != nil {
			return err
		}
		frec = f
		return nil
	})
	if err != nil {
		return nil, err
	}

	chunker := cc.db.resolveChunker(frec.Strategy)
	if chunker == nil {
		return nil, fmt.Errorf("chunker strategy %q not registered", frec.Strategy)
	}

	// R514: dispatch — FileChunker reads the file itself, Chunker needs os.ReadFile
	var data []byte
	if _, ok := chunker.(FileChunker); !ok {
		data, err = os.ReadFile(fpath)
		if err != nil {
			return nil, err
		}
	}

	rangeIds := make(map[string]uint64, len(frec.Chunks))
	for _, fce := range frec.Chunks {
		rangeIds[fce.Location] = fce.ChunkID
	}

	cf := &cachedFile{
		path:       fpath,
		data:       data,
		chunker:    chunker,
		fileChunks: frec.Chunks,
		rangeIds:   rangeIds,
		byRange:    make(map[string]int, len(frec.Chunks)),
	}
	cc.files[fpath] = cf
	return cf, nil
}

func (cf *cachedFile) lookupLocation(chunkID uint64) (string, bool) {
	for _, fce := range cf.fileChunks {
		if fce.ChunkID == chunkID {
			return fce.Location, true
		}
	}
	return "", false
}

// retrieveFast runs the RandomAccessChunker path for a single chunk. R542
func (cc *ChunkCache) retrieveFast(cf *cachedFile, chunkID uint64, loc string, ra RandomAccessChunker) bool {
	var attrs []Pair
	err := cc.db.env.View(func(txn *lmdb.Txn) error {
		crec, err := cc.db.readCRecord(txn, chunkID)
		if err != nil {
			return err
		}
		attrs = crec.Attrs
		return nil
	})
	if err != nil {
		return false
	}

	chunk := Chunk{Range: []byte(loc), Attrs: attrs}
	if err := ra.GetChunk(cf.path, cf.data, &cf.customData, &chunk); err != nil {
		return false
	}
	cc.storeChunk(cf, chunk)
	return true
}

// populateFastWindow batch-reads C records for all uncached chunks in [lo, hi]
// in one View txn, then dispatches GetChunk for each. R544
func (cc *ChunkCache) populateFastWindow(cf *cachedFile, ra RandomAccessChunker, lo, hi int) error {
	type todo struct {
		chunkID uint64
		loc     string
	}
	var pending []todo
	for i := lo; i <= hi; i++ {
		fce := cf.fileChunks[i]
		if _, ok := cf.byRange[fce.Location]; !ok {
			pending = append(pending, todo{fce.ChunkID, fce.Location})
		}
	}
	if len(pending) == 0 {
		return nil
	}

	attrsByID := make(map[uint64][]Pair, len(pending))
	err := cc.db.env.View(func(txn *lmdb.Txn) error {
		for _, t := range pending {
			crec, err := cc.db.readCRecord(txn, t.chunkID)
			if err != nil {
				return fmt.Errorf("read C record %d: %w", t.chunkID, err)
			}
			attrsByID[t.chunkID] = crec.Attrs
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, t := range pending {
		chunk := Chunk{Range: []byte(t.loc), Attrs: attrsByID[t.chunkID]}
		if err := ra.GetChunk(cf.path, cf.data, &cf.customData, &chunk); err != nil {
			return fmt.Errorf("GetChunk %s %s: %w", cf.path, t.loc, err)
		}
		cc.storeChunk(cf, chunk)
	}
	return nil
}

// retrieveStream streams the chunker from the start, caching each yielded chunk,
// stopping when the target range is found. R543
func (cc *ChunkCache) retrieveStream(cf *cachedFile, rangeLabel string) ([]byte, bool) {
	var result []byte
	var found bool
	cc.runChunker(cf, func(c Chunk) bool {
		if _, ok := cf.byRange[string(c.Range)]; !ok {
			cc.storeChunk(cf, c)
		}
		if string(c.Range) == rangeLabel {
			result = cf.chunks[cf.byRange[rangeLabel]].Content
			found = true
			return false
		}
		return true
	})
	if !found {
		cf.complete = true
	}
	return result, found
}

// chunkFull runs the streaming chunker to completion, caching every yielded chunk. R514
func (cc *ChunkCache) chunkFull(cf *cachedFile) {
	cc.runChunker(cf, func(c Chunk) bool {
		if _, ok := cf.byRange[string(c.Range)]; !ok {
			cc.storeChunk(cf, c)
		}
		return true
	})
	cf.complete = true
}

// runChunker dispatches streaming chunking based on the chunker's interface.
func (cc *ChunkCache) runChunker(cf *cachedFile, yield func(Chunk) bool) {
	if fc, ok := cf.chunker.(FileChunker); ok {
		fc.FileChunks(cf.path, [32]byte{}, yield)
		return
	}
	if ch, ok := cf.chunker.(Chunker); ok {
		ch.Chunks(cf.path, cf.data, yield)
	}
}

// storeChunk deep-copies a chunk and appends to the access-order cache. R545
// Caller's chunk may reference reusable buffers; the cache owns its own copies.
func (cc *ChunkCache) storeChunk(cf *cachedFile, c Chunk) {
	rangeStr := string(c.Range)
	content := make([]byte, len(c.Content))
	copy(content, c.Content)
	idx := len(cf.chunks)
	cf.chunks = append(cf.chunks, cachedChunk{
		Range:   rangeStr,
		Content: content,
		Attrs:   CopyPairs(c.Attrs),
	})
	cf.byRange[rangeStr] = idx
}
