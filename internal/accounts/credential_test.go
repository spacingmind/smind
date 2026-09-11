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

func TestOAuthCredentialUnmarshalJSONExpiresAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		json    string
		want    time.Time
		wantErr bool
	}{
		{
			name: "rfc3339 string",
			json: `{"access_token":"a","refresh_token":"r","expires_at":"2030-01-02T03:04:05Z"}`,
			want: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		{
			name: "epoch millis number",
			// 1789156725672 ms == 2026-09-11T03:38:45.672Z, the kind of
			// value Claude Code sends for expires_at.
			json: `{"access_token":"a","refresh_token":"r","expires_at":1789156725672}`,
			want: time.UnixMilli(1789156725672).UTC(),
		},
		{
			name: "epoch seconds number",
			json: `{"access_token":"a","refresh_token":"r","expires_at":1789156725}`,
			want: time.Unix(1789156725, 0).UTC(),
		},
		{
			name:    "invalid string",
			json:    `{"access_token":"a","refresh_token":"r","expires_at":"not-a-timestamp"}`,
			wantErr: true,
		},
		{
			name:    "invalid type",
			json:    `{"access_token":"a","refresh_token":"r","expires_at":true}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got OAuthCredential
			err := json.Unmarshal([]byte(tt.json), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if !got.ExpiresAt.Equal(tt.want) {
				t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, tt.want)
			}
			if got.AccessToken != "a" || got.RefreshToken != "r" {
				t.Errorf("AccessToken/RefreshToken not decoded: %+v", got)
			}
		})
	}
}
