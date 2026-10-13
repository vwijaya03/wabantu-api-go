package ai

import (
	"database/sql"

	"encore.dev/storage/sqldb"
)

// llmSystemDB references the system DB (ai_providers / ai_function_config live in schema public).
var llmSystemDB = sqldb.Named("system")

func llmConfigDB() *sql.DB { return llmSystemDB.Stdlib() }
