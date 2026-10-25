// Command microfts2-migrate-v3 brings a microfts2 database from format "2" to
// format "3" by giving every F record a zero chunkerVersion. Run it once per
// database; microfts2.Open refuses format "2".
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"

	"go.etcd.io/bbolt"
)

// CRC: crc-MigrateV3.md | R722, R725
func main() {
	dbPath := flag.String("db", "", "path to the bbolt database file (required)")
	bucket := flag.String("bucket", "fts", "microfts2 bucket name")
	flag.Parse()
	if *dbPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := bbolt.Open(*dbPath, 0644, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	n, already, err := migrate(db, *bucket)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	case already:
		fmt.Printf("bucket %q is already format 3; nothing to do\n", *bucket)
	default:
		fmt.Printf("bucket %q migrated to format 3: %d F records rewritten\n", *bucket, n)
	}
}

var versionKey = []byte("Iversion")

// migrate rewrites every F record of a format-"2" bucket in one transaction and
// sets the format to "3". A format-"3" bucket is left alone (already = true);
// any other format is refused. A failure leaves the database untouched.
// CRC: crc-MigrateV3.md | R723, R724
func migrate(db *bbolt.DB, bucket string) (rewritten int, already bool, err error) {
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %q not found", bucket)
		}
		switch v := string(b.Get(versionKey)); v {
		case "3":
			already = true
			return nil
		case "2":
		default:
			return fmt.Errorf("bucket %q: format version %q, can only migrate \"2\"", bucket, v)
		}
		// Collect first: bbolt forbids Put while a cursor walks the same bucket.
		type rec struct{ k, v []byte }
		var recs []rec
		c := b.Cursor()
		for k, v := c.Seek([]byte{'F'}); k != nil && k[0] == 'F'; k, v = c.Next() {
			nv, err := spliceVersion(v)
			if err != nil {
				return fmt.Errorf("F record %x: %w", k, err)
			}
			recs = append(recs, rec{append([]byte(nil), k...), nv})
		}
		for _, r := range recs {
			if err := b.Put(r.k, r.v); err != nil {
				return err
			}
		}
		rewritten = len(recs)
		return b.Put(versionKey, []byte("3"))
	})
	return rewritten, already, err
}

// spliceVersion returns a format-"2" F record value with a zero chunkerVersion
// varint inserted after the strategy string:
// [modTime:8] [contentHash:32] [fileLength:varint] [strategy:str] | 0x00 | rest.
// CRC: crc-MigrateV3.md | R723
func spliceVersion(v []byte) ([]byte, error) {
	off := 8 + 32
	if len(v) < off {
		return nil, fmt.Errorf("value too short: %d bytes", len(v))
	}
	_, n := binary.Uvarint(v[off:])
	if n <= 0 {
		return nil, fmt.Errorf("bad fileLength")
	}
	off += n
	l, n := binary.Uvarint(v[off:])
	if n <= 0 || l > uint64(len(v)-off-n) {
		return nil, fmt.Errorf("bad strategy")
	}
	off += n + int(l)
	out := make([]byte, 0, len(v)+1)
	out = append(out, v[:off]...)
	out = append(out, 0)
	return append(out, v[off:]...), nil
}
