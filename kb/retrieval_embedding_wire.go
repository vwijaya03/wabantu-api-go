package kb

import (
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/llm"
	"encore.app/wabantu/shared/retrieval"
)

// systemDB hosts ai_function_config (function "embedding"). shared/retrieval is a library and
// cannot declare infrastructure, so every process that imports kb (ai, admin, flag, business)
// gets the active-embedding-model resolver wired here.
var systemDB = sqldb.Named("system")

func init() {
	retrieval.SetSystemDBProvider(func() llm.Querier { return systemDB.Stdlib() })
}
