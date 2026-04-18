package microfts2

// CRC: crc-ChunkCache.md | Seq: seq-cache.md | R297, R298, R299, R300, R301, R302, R303, R304, R305, R306

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
	path     string
	data     []byte // nil for FileChunker (chunker reads file directly)
	chunker  any    // Chunker or FileChunker
	chunks   []cachedChunk // sparse — zero-value entries not yet retrieved
	byRange  map[string]int
	complete bool
}

type cachedChunk struct {
	Range   string
	Content []byte
	Attrs   []Pair
	valid   bool // distinguishes "not yet seen" from "seen, empty content"
}

// NewChunkCache creates a per-query chunk cache.
func (db *DB) NewChunkCache() *ChunkCache {
	return &ChunkCache{
		db:    db,
		files: make(map[string]*cachedFile),
	}
}

// ChunkText returns a single chunk's content by range label.
// Uses lazy chunking — stops as soon as the target is found.
func (cc *ChunkCache) ChunkText(fpath, rangeLabel string) ([]byte, bool) {
	cf, err := cc.ensureFile(fpath)
	if err != nil {
		return nil, false
	}

	// Check if already cached.
	if idx, ok := cf.byRange[rangeLabel]; ok {
		return cf.chunks[idx].Content, true
	}

	// If fully chunked and not found, it doesn't exist.
	if cf.complete {
		return nil, false
	}

	// Lazy chunk until we find it.
	return cc.chunkUntil(cf, rangeLabel)
}

// GetChunks retrieves the target chunk and up to before/after positional neighbors.
// Same contract as DB.GetChunks but cached.
func (cc *ChunkCache) GetChunks(fpath, targetRange string, before, after int) ([]ChunkResult, error) {
	cf, err := cc.ensureFile(fpath)
	if err != nil {
		return nil, err
	}

	// Full chunk if not already done.
	if !cf.complete {
		cc.chunkFull(cf)
	}

	// Find target.
	targetIdx, ok := cf.byRange[targetRange]
	if !ok {
		return nil, fmt.Errorf("range %q not found in %s", targetRange, fpath)
	}

	lo := max(0, targetIdx-before)
	hi := min(len(cf.chunks)-1, targetIdx+after)

	var results []ChunkResult
	for i := lo; i <= hi; i++ {
		ch := cf.chunks[i]
		if !ch.valid {
			continue
		}
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

	// Resolve path → fileid, read F record.
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
		var err error
		data, err = os.ReadFile(fpath)
		if err != nil {
			return nil, err
		}
	}

	cf := &cachedFile{
		path:    fpath,
		data:    data,
		chunker: chunker,
		chunks:  make([]cachedChunk, len(frec.Chunks)),
		byRange: make(map[string]int, len(frec.Chunks)),
	}
	cc.files[fpath] = cf
	return cf, nil
}

// chunkFull runs the chunker to completion, filling every slot. R514
func (cc *ChunkCache) chunkFull(cf *cachedFile) {
	idx := 0
	yield := func(c Chunk) bool {
		if !cf.chunkAt(idx) {
			cc.storeChunk(cf, idx, c)
		}
		idx++
		return true
	}
	if fc, ok := cf.chunker.(FileChunker); ok {
		fc.FileChunks(cf.path, [32]byte{}, yield)
	} else if ch, ok := cf.chunker.(Chunker); ok {
		ch.Chunks(cf.path, cf.data, yield)
	}
	cf.complete = true
}

// chunkUntil runs the chunker from the start, caching each unseen chunk,
// stopping when the target range is found. R514
func (cc *ChunkCache) chunkUntil(cf *cachedFile, rangeLabel string) ([]byte, bool) {
	var result []byte
	var found bool
	idx := 0
	yield := func(c Chunk) bool {
		if !cf.chunkAt(idx) {
			cc.storeChunk(cf, idx, c)
		}
		if string(c.Range) == rangeLabel {
			result = cf.chunks[idx].Content
			found = true
			idx++
			return false
		}
		idx++
		return true
	}
	if fc, ok := cf.chunker.(FileChunker); ok {
		fc.FileChunks(cf.path, [32]byte{}, yield)
	} else if ch, ok := cf.chunker.(Chunker); ok {
		ch.Chunks(cf.path, cf.data, yield)
	}
	if !found {
		cf.complete = true
	}
	return result, found
}

// storeChunk deep-copies a chunk into the cache at the given index.
func (cc *ChunkCache) storeChunk(cf *cachedFile, idx int, c Chunk) {
	// Grow if needed (chunker may produce more chunks than F record listed).
	for idx >= len(cf.chunks) {
		cf.chunks = append(cf.chunks, cachedChunk{})
	}

	rangeStr := string(c.Range)
	content := make([]byte, len(c.Content))
	copy(content, c.Content)

	cf.chunks[idx] = cachedChunk{
		Range:   rangeStr,
		Content: content,
		Attrs:   CopyPairs(c.Attrs),
		valid:   true,
	}
	cf.byRange[rangeStr] = idx
}

// chunkAt reports whether a chunk at the given index is already cached.
func (cf *cachedFile) chunkAt(idx int) bool {
	return idx < len(cf.chunks) && cf.chunks[idx].valid
}
