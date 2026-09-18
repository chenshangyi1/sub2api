package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptivePoolUpdateValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   AdaptivePoolUpdate
		wantErr string
	}{
		{
			name: "valid enabled pool",
			input: AdaptivePoolUpdate{
				ParentGroupID: 10,
				Enabled:       true,
				Members: []AdaptiveLeafRef{
					{LeafGroupID: 21, Enabled: true, SortOrder: 10},
					{LeafGroupID: 22, Enabled: false, SortOrder: 20},
				},
			},
		},
		{
			name:    "parent must be positive",
			input:   AdaptivePoolUpdate{Enabled: false},
			wantErr: "parent group id must be positive",
		},
		{
			name:    "enabled pool needs a member",
			input:   AdaptivePoolUpdate{ParentGroupID: 10, Enabled: true},
			wantErr: "requires at least one leaf group",
		},
		{
			name: "enabled pool needs enabled member",
			input: AdaptivePoolUpdate{
				ParentGroupID: 10,
				Enabled:       true,
				Members:       []AdaptiveLeafRef{{LeafGroupID: 21}},
			},
			wantErr: "requires an enabled leaf group",
		},
		{
			name: "self reference",
			input: AdaptivePoolUpdate{
				ParentGroupID: 10,
				Members:       []AdaptiveLeafRef{{LeafGroupID: 10}},
			},
			wantErr: "cannot reference itself",
		},
		{
			name: "duplicate leaf",
			input: AdaptivePoolUpdate{
				ParentGroupID: 10,
				Members: []AdaptiveLeafRef{
					{LeafGroupID: 21},
					{LeafGroupID: 21},
				},
			},
			wantErr: "duplicate adaptive leaf group id: 21",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
