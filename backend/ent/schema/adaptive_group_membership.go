package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdaptiveGroupMembership links one Adaptive parent configuration to an
// eligible leaf group. It does not duplicate leaf model capabilities.
type AdaptiveGroupMembership struct {
	ent.Schema
}

func (AdaptiveGroupMembership) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "adaptive_group_memberships"},
	}
}

func (AdaptiveGroupMembership) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (AdaptiveGroupMembership) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("config_id").
			Positive(),
		field.Int64("leaf_group_id").
			Positive(),
		field.Bool("enabled").
			Default(true),
		field.Int("sort_order").
			Default(0),
	}
}

func (AdaptiveGroupMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("config", AdaptiveGroupConfig.Type).
			Field("config_id").
			Unique().
			Required(),
		edge.To("leaf_group", Group.Type).
			Field("leaf_group_id").
			Unique().
			Required(),
	}
}

func (AdaptiveGroupMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("config_id", "leaf_group_id").Unique(),
		index.Fields("config_id", "enabled", "sort_order"),
		index.Fields("leaf_group_id"),
	}
}
