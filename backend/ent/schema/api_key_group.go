package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ApiKeyGroup stores the groups explicitly selected for an API key.
type ApiKeyGroup struct {
	ent.Schema
}

func (ApiKeyGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "api_key_groups"},
		field.ID("api_key_id", "group_id"),
	}
}

func (ApiKeyGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("api_key_id"),
		field.Int64("group_id"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ApiKeyGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_key", APIKey.Type).
			Unique().
			Required().
			Field("api_key_id"),
		edge.To("group", Group.Type).
			Unique().
			Required().
			Field("group_id"),
	}
}

func (ApiKeyGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id"),
	}
}
