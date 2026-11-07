package main

import (
	"database/sql"

	"boilerplate/api/internal/platform/db/sqlcgen"
)

func queriesOf(pool *sql.DB) *sqlcgen.Queries {
	return sqlcgen.New(pool)
}
