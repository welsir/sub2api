// [INPUT]: Ent schema primitives, shared time mixin, users, and user subscriptions.
// [OUTPUT]: UserActivationJourney persistence model and typed relationship metadata.
// [POS]: Auditable lifecycle record for HVOY new-user activation without deciding eligibility.
//
// [PROTOCOL]:
// 1. Update this header when fields, relationships, or lifecycle states change.
// 2. Keep the matching SQL migration and generated Ent code aligned.
package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserActivationJourney stores observable activation lifecycle state for one user.
type UserActivationJourney struct {
	ent.Schema
}

func (UserActivationJourney) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_activation_journeys"},
	}
}

func (UserActivationJourney) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (UserActivationJourney) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.String("campaign_source").
			MaxLen(64).
			Default("direct"),
		field.Enum("starter_state").
			Values("pending", "granted", "expired").
			Default("pending").
			Comment("Observable starter lifecycle state; eligibility must be re-evaluated from current evidence."),
		field.Int64("starter_subscription_id").
			Optional().
			Nillable(),
		field.Time("starter_granted_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("starter_expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Enum("recall_state").
			Values("locked", "claimable", "claimed", "expired", "blocked_paid", "closed_success").
			Default("locked").
			Comment("Observable recall lifecycle state; eligibility must be re-evaluated from current evidence."),
		field.Int64("recall_subscription_id").
			Optional().
			Nillable(),
		field.Time("recall_claimed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("recall_expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("first_success_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("no_attempt_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("attempted_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("paid_support_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("recall_available_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("recall_expired_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_email_sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_evaluated_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserActivationJourney) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("activation_journey").
			Field("user_id").
			Unique().
			Required(),
		edge.To("starter_subscription", UserSubscription.Type).
			Field("starter_subscription_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("recall_subscription", UserSubscription.Type).
			Field("recall_subscription_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (UserActivationJourney) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
		index.Fields("campaign_source", "created_at"),
		index.Fields("starter_expires_at"),
		index.Fields("recall_state"),
	}
}
