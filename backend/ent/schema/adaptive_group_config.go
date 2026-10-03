package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// AdaptiveGroupConfig marks a normal public group as an Adaptive parent.
// Its platform remains authoritative on the related Group row.
type AdaptiveGroupConfig struct {
	ent.Schema
}

func (AdaptiveGroupConfig) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "adaptive_group_configs"},
	}
}

func (AdaptiveGroupConfig) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (AdaptiveGroupConfig) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("parent_group_id").
			Positive().
			Unique(),
		field.Bool("enabled").
			Default(true),
		field.Int64("config_generation").
			Positive().
			Default(1).
			Comment("Monotonic topology generation frozen into Adaptive route plans"),
	}
}

func (AdaptiveGroupConfig) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("parent_group", Group.Type).
			Field("parent_group_id").
			Unique().
			Required(),
		edge.From("members", AdaptiveGroupMembership.Type).
			Ref("config"),
	}
}
