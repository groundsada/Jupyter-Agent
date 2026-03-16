package hubclient

import (
	"testing"
)

func TestExtractIPFromServerURL(t *testing.T) {
	cases := []struct {
		url     string
		wantIP  string
		wantErr bool
	}{
		{"http://10.0.1.42:8888/user/alice/", "10.0.1.42", false},
		{"http://192.168.1.5:8888/user/bob/", "192.168.1.5", false},
		{"https://10.0.0.1:443/user/carol/", "10.0.0.1", false},
		{"http://10.0.1.42/user/alice/", "10.0.1.42", false},
		{"ftp://10.0.0.1/path", "", true},
		{"", "", true},
	}

	for _, tc := range cases {
		ip, err := extractIPFromServerURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("extractIPFromServerURL(%q): expected error, got ip=%q", tc.url, ip)
			}
			continue
		}
		if err != nil {
			t.Errorf("extractIPFromServerURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if ip != tc.wantIP {
			t.Errorf("extractIPFromServerURL(%q): got %q, want %q", tc.url, ip, tc.wantIP)
		}
	}
}
