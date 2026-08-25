package db

// Qualify returns a schema-qualified table identifier: "schema"."table".
func Qualify(schema, table string) string {
	return QuoteIdent(schema) + "." + QuoteIdent(table)
}

// SchemaSQL helps build tenant-scoped SQL without SET search_path.
type SchemaSQL struct {
	Schema string
}

// T qualifies a table name in this tenant schema.
func (s SchemaSQL) T(table string) string {
	return Qualify(s.Schema, table)
}
