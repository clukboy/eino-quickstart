package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RunRetrievalHit stores the retrieval evidence shown in a run detail page.
type RunRetrievalHit struct {
	ent.Schema
}

func (RunRetrievalHit) Fields() []ent.Field {
	return []ent.Field{
		field.String("run_id").NotEmpty(),
		field.Uint64("chunk_id").Default(0),
		field.Uint64("document_id").Default(0),
		field.String("source").Default(""),
		field.String("title").Default(""),
		field.String("heading_path").Default(""),
		field.Text("content").Default(""),
		field.Float("score").Default(0),
		field.Bool("content_truncated").Default(false),
		field.Int("rank").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RunRetrievalHit) Edges() []ent.Edge { return nil }

func (RunRetrievalHit) Indexes() []ent.Index {
	return []ent.Index{index.Fields("run_id", "rank"), index.Fields("document_id")}
}
