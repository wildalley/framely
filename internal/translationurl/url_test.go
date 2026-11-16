package translationurl

import "testing"

func TestTranslationURLTransportPolicy(t *testing.T) {
	for _, raw := range []string{"https://api.example.com/v1", "http://localhost:8080/v1", "http://LOCALHOST.:8080", "http://127.0.0.1:8080", "http://127.1.2.3", "http://[::1]:8080", "http://[::ffff:127.0.0.1]:8080"} {
		if err := Validate(raw); err != nil {
			t.Errorf("local/HTTPS endpoint %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "/v1", "http://public.example.com", "http://192.168.1.2", "http://0.0.0.0", "http://localhost.example.com", "http://127.0.0.1.example.com", "http://[::]", "ftp://localhost", "https://user:password@example.com", "https:///v1"} {
		if err := Validate(raw); err == nil {
			t.Errorf("unsafe endpoint %q accepted", raw)
		}
	}
}
