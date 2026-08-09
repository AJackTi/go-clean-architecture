// Command migrate applies or rolls back database migrations.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	action, argument, err := command(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	sourceURL := os.Getenv("MIGRATIONS_PATH")
	if sourceURL == "" {
		sourceURL = "file://db/migrations"
	}

	db, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		log.Fatalf("open migration database: %v", err)
	}
	defer func() {
		_, _ = db.Close()
	}()

	switch action {
	case "up":
		err = db.Up()
	case "down":
		err = db.Steps(-1)
	case "step":
		err = db.Steps(argument)
	case "version":
		version, dirty, versionErr := db.Version()
		if versionErr != nil && !errors.Is(versionErr, migrate.ErrNilVersion) {
			log.Fatalf("read migration version: %v", versionErr)
		}
		fmt.Printf("version=%d dirty=%t\n", version, dirty)
		return
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) && !errors.Is(err, migrate.ErrNilVersion) {
		log.Fatalf("migration failed: %v", err)
	}
}

func command(args []string) (action string, argument int, err error) {
	action = "up"
	if len(args) == 0 {
		return action, 0, nil
	}
	if len(args) > 2 {
		return "", 0, fmt.Errorf("usage: migrate [up|down|step N|version]")
	}
	action = args[0]
	switch action {
	case "up", "down", "version":
		if len(args) != 1 {
			return "", 0, fmt.Errorf("usage: migrate [up|down|step N|version]")
		}
		return action, 0, nil
	case "step":
		// Parsed below.
	default:
		return "", 0, fmt.Errorf("usage: migrate [up|down|step N|version]")
	}
	if len(args) != 2 {
		return "", 0, fmt.Errorf("step requires an integer argument")
	}
	argument, err = strconv.Atoi(args[1])
	if err != nil || argument == 0 {
		return "", 0, fmt.Errorf("step argument must be a non-zero integer")
	}
	return action, argument, nil
}
