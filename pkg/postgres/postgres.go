// Package postgres implements postgres connection.
package postgres

import (
	"fmt"

	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/postgresql"
)

// Postgres -.
type Postgres db.Session

// New -.
func New(cfg *config.Config) (Postgres, error) {
	h := fmt.Sprintf("%s:%s", cfg.PG.Host, cfg.PG.Port)

	sess, err := postgresql.Open(postgresql.ConnectionURL{
		Database: cfg.PG.DbName,
		Host:     h,
		User:     cfg.PG.User,
		Password: cfg.PG.Password,
		Options: map[string]string{
			"sslmode": cfg.PG.SSLMode,
		},
	})

	if err != nil {
		return nil, err
	}

	newVar := sess.(Postgres)
	return newVar, nil
}
