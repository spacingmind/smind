package accounts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCredentialRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cred any
	}{
		{
			name: "api key",
			cred: APIKeyCredential{Key: "sk-test-123"},
		},
		{
			name: "oauth",
			cred: OAuthCredential{
				AccessToken:  "access-abc",
				RefreshToken: "refresh-xyz",
				ExpiresAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.cred)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			switch want := tt.cred.(type) {
			case APIKeyCredential:
				var got APIKeyCredential
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatalf("Unmarshal() error = %v", err)
				}
				if got != want {
					t.Errorf("round trip = %+v, want %+v", got, want)
				}
			case OAuthCredential:
				var got OAuthCredential
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatalf("Unmarshal() error = %v", err)
				}
				if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.ExpiresAt.Equal(want.ExpiresAt) {
					t.Errorf("round trip = %+v, want %+v", got, want)
				}
			}
		})
	}
}
