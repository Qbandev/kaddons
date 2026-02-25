package fetch

import "testing"

func TestValidatePublicHTTPSURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "public https domain", rawURL: "https://example.com/docs", wantErr: false},
		{name: "public https ip", rawURL: "https://1.1.1.1/docs", wantErr: false},
		{name: "http blocked", rawURL: "http://example.com/docs", wantErr: true},
		{name: "localhost blocked", rawURL: "https://localhost/docs", wantErr: true},
		{name: "local tld blocked", rawURL: "https://cluster.local/docs", wantErr: true},
		{name: "private ip blocked", rawURL: "https://10.0.0.5/docs", wantErr: true},
		{name: "loopback blocked", rawURL: "https://127.0.0.1/docs", wantErr: true},
		{name: "invalid blocked", rawURL: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePublicHTTPSURL(tt.rawURL)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePublicHTTPSURL(%q) expected error", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePublicHTTPSURL(%q) unexpected error: %v", tt.rawURL, err)
			}
		})
	}
}
