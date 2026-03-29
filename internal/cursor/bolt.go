package cursor

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"time"

	"ton-monitoring/internal/domain"

	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("cursors")

type Store struct {
	db *bolt.DB
}

func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{
		Timeout:      time.Second,
		FreelistType: bolt.FreelistMapType,
	})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Load(accountID string) (domain.Cursor, error) {
	var c domain.Cursor
	c.AccountID = accountID

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		v := b.Get([]byte(accountID))
		if v == nil {
			return nil
		}
		if len(v) < 8 {
			return nil
		}
		c.Lt = binary.BigEndian.Uint64(v[:8])
		c.TxHash = string(v[8:])
		return nil
	})

	return c, err
}

func (s *Store) Save(c domain.Cursor) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		val := make([]byte, 8+len(c.TxHash))
		binary.BigEndian.PutUint64(val[:8], c.Lt)
		copy(val[8:], c.TxHash)
		return b.Put([]byte(c.AccountID), val)
	})
}

func (s *Store) LoadAll() ([]domain.Cursor, error) {
	var cursors []domain.Cursor
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		return b.ForEach(func(k, v []byte) error {
			if len(v) < 8 {
				return nil
			}
			cursors = append(cursors, domain.Cursor{
				AccountID: string(k),
				Lt:        binary.BigEndian.Uint64(v[:8]),
				TxHash:    string(v[8:]),
			})
			return nil
		})
	})
	return cursors, err
}

func (s *Store) Close() error {
	return s.db.Close()
}
