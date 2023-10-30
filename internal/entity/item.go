package entity

import (
	"github.com/AJackTi/go-clean-architecture/pkg/postgres"
	"github.com/upper/db/v4"
)

// ItemStore represents a pool of Item
type ItemStore struct {
	db.Collection
}

const ItemTableName = "Items"

func Items(sess postgres.Postgres) *ItemStore {
	return &ItemStore{sess.Collection(ItemTableName)}
}

// Item entity
type Item struct {
}

func (e *Item) Store(sess db.Session) db.Store {
	return sess.Collection(ItemTableName)
}

func (e *Item) ToRecord() db.Record {
	return db.Record(e)
}

func (s *ItemStore) Create(item *Item) (*Item, error) {
	if err := s.Session().Save(item.ToRecord()); err != nil {
		return nil, err
	}

	return item, nil
}
