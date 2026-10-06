package postgres

import (
	"eino-quickstart/ent/documentchunk"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

func TestMetadataContainsFoldBuildsPostgresPredicate(t *testing.T) {
	tests := []struct {
		name       string
		term       string
		wantArg    string
		wantClause string
	}{
		{
			name:       "plain metadata value",
			term:       "H105P",
			wantArg:    "%H105P%",
			wantClause: `"document_chunks"."metadata"::text ILIKE $1 ESCAPE '\\'`,
		},
		{
			name:       "like wildcards stay literal",
			term:       `100%_ready\now`,
			wantArg:    `%100\%\_ready\\now%`,
			wantClause: `"document_chunks"."metadata"::text ILIKE $1 ESCAPE '\\'`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector := sql.Dialect(dialect.Postgres).
				Select("*").
				From(sql.Table(documentchunk.Table))
			metadataContainsFold(test.term)(selector)

			query, args := selector.Query()
			if !strings.Contains(query, test.wantClause) {
				t.Fatalf("query = %q, want clause %q", query, test.wantClause)
			}
			if len(args) != 1 || args[0] != test.wantArg {
				t.Fatalf("args = %#v, want [%q]", args, test.wantArg)
			}
		})
	}
}
