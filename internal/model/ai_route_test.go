package model

import "testing"

func TestGenerateAIRouteRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     GenerateAIRouteRequest
		wantErr bool
	}{
		{
			name: "group scope requires an explicit target",
			req: GenerateAIRouteRequest{
				Scope:   AIRouteScopeGroup,
				GroupID: 0,
			},
			wantErr: true,
		},
		{
			name: "group scope is valid",
			req: GenerateAIRouteRequest{
				Scope:   AIRouteScopeGroup,
				GroupID: 1,
			},
		},
		{
			name: "table scope accepts category strategies",
			req: GenerateAIRouteRequest{
				Scope:            AIRouteScopeTable,
				CategoryStrategy: AIRouteCategoryChannelTier,
			},
		},
		{
			name: "unknown category strategy is invalid",
			req: GenerateAIRouteRequest{
				Scope:            AIRouteScopeTable,
				CategoryStrategy: AIRouteCategoryStrategy("typo"),
			},
			wantErr: true,
		},
		{
			name:    "empty scope is invalid",
			req:     GenerateAIRouteRequest{},
			wantErr: true,
		},
		{
			name: "unknown scope is invalid",
			req: GenerateAIRouteRequest{
				Scope: AIRouteScope("typo"),
			},
			wantErr: true,
		},
		{
			name: "negative group id is invalid",
			req: GenerateAIRouteRequest{
				Scope:   AIRouteScopeGroup,
				GroupID: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
