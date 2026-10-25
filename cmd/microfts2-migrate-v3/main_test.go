package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zot/microfts2"
	"go.etcd.io/bbolt"
)

const longStrategy = "a-strategy-name-long-enough-to-need-more-than-one-varint-byte-for-its-length-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

// v2Fixture builds a format-"3" database with two files, records their F records,
// then downgrades it to format "2" by removing each record's chunkerVersion byte.
// The offset is computed from the known field values, independently of spliceVersion.
func v2Fixture(t *testing.T) (string, map[uint64]microfts2.FRecord, map[string]bool) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db")
	db, err := microfts2.Create(dbPath, microfts2.Options{})
	if err != nil {
		t.Fatal(err)
	}
	db.AddStrategyFunc("line", microfts2.LineChunkFunc)
	db.AddStrategyFunc(longStrategy, microfts2.LineChunkFunc)
	want := map[uint64]microfts2.FRecord{}
	paths := map[string]bool{}
	for i, strat := range []string{"line", longStrategy} {
		p := filepath.Join(dir, []string{"a.txt", "b.txt"}[i])
		os.WriteFile(p, []byte("alpha\nbeta\ngamma\n"), 0644)
		fid, err := db.AddFile(p, strat)
		if err != nil {
			t.Fatal(err)
		}
		frec, err := db.FileInfoByID(fid)
		if err != nil {
			t.Fatal(err)
		}
		want[fid] = frec
		paths[p] = true
	}
	err = db.DB().Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("fts"))
		for fid, frec := range want {
			key := binary.AppendUvarint([]byte{'F'}, fid)
			v := b.Get(key)
			off := 40 + len(binary.AppendUvarint(nil, uint64(frec.FileLength))) +
				len(binary.AppendUvarint(nil, uint64(len(frec.Strategy)))) + len(frec.Strategy)
			if v[off] != 0 {
				t.Fatalf("fixture: expected a zero chunkerVersion at offset %d", off)
			}
			old := append(append([]byte(nil), v[:off]...), v[off+1:]...)
			if err := b.Put(key, old); err != nil {
				return err
			}
		}
		return b.Put(versionKey, []byte("2"))
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	return dbPath, want, paths
}

func openBolt(t *testing.T, path string) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(path, 0644, nil)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// snapshot copies every key and value of the bucket.
func snapshot(t *testing.T, db *bbolt.DB) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte("fts")).ForEach(func(k, v []byte) error {
			out[string(k)] = append([]byte(nil), v...)
			return nil
		})
	})
	return out
}

// CRC: crc-MigrateV3.md | Test: test-MigrateV3.md | R723, R725
func TestMigrateV2(t *testing.T) {
	path, want, paths := v2Fixture(t)
	bdb := openBolt(t, path)
	n, already, err := migrate(bdb, "fts")
	bdb.Close()
	if err != nil || already || n != 2 {
		t.Fatalf("migrate = (%d, %v, %v), want (2, false, nil)", n, already, err)
	}
	db, err := microfts2.Open(path, microfts2.Options{})
	if err != nil {
		t.Fatalf("Open after migrate: %v", err)
	}
	defer db.Close()
	if v, _ := db.Version(); v != "3" {
		t.Fatalf("Version() = %q, want 3", v)
	}
	db.AddStrategyFunc("line", microfts2.LineChunkFunc)
	db.AddStrategyFunc(longStrategy, microfts2.LineChunkFunc)
	for fid, w := range want {
		got, err := db.FileInfoByID(fid)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("fileid %d after migrate:\n got %+v\nwant %+v", fid, got, w)
		}
	}
	for p := range paths {
		st, err := db.CheckFile(p)
		if err != nil || st.Status != "fresh" {
			t.Errorf("CheckFile(%s) = %+v, %v; want fresh", p, st, err)
		}
	}
}

// CRC: crc-MigrateV3.md | Test: test-MigrateV3.md | R724
func TestMigrateTwice(t *testing.T) {
	path, _, _ := v2Fixture(t)
	bdb := openBolt(t, path)
	defer bdb.Close()
	if _, _, err := migrate(bdb, "fts"); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, bdb)
	n, already, err := migrate(bdb, "fts")
	if err != nil || !already || n != 0 {
		t.Fatalf("second migrate = (%d, %v, %v), want (0, true, nil)", n, already, err)
	}
	if !reflect.DeepEqual(snapshot(t, bdb), first) {
		t.Fatal("second migrate changed the bucket")
	}
}

// CRC: crc-MigrateV3.md | Test: test-MigrateV3.md | R723
func TestMigrateRefusesUnknownVersion(t *testing.T) {
	path, _, _ := v2Fixture(t)
	bdb := openBolt(t, path)
	defer bdb.Close()
	bdb.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte("fts")).Put(versionKey, []byte("1"))
	})
	before := snapshot(t, bdb)
	_, _, err := migrate(bdb, "fts")
	if err == nil || !strings.Contains(err.Error(), `"1"`) {
		t.Fatalf("migrate on version 1: err = %v, want an error naming \"1\"", err)
	}
	after := snapshot(t, bdb)
	if len(after) != len(before) {
		t.Fatal("refused migrate changed the bucket")
	}
	for k, v := range before {
		if !bytes.Equal(after[k], v) {
			t.Fatalf("refused migrate changed key %q", k)
		}
	}
}
