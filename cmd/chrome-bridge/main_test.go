package main

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.1:9223", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost:9223", true},
		{"[::1]:9223", true},
		{"::1", true},
		{"192.168.1.1", false},
		{"attacker.example", false},
		{"attacker.example:9223", false},
		{"", false},
		{"10.0.0.1:9223", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestIsLoopbackOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://127.0.0.1:9223", true},
		{"http://localhost", true},
		{"https://[::1]:9223", true},
		{"http://attacker.example", false},
		{"https://attacker.example", false},
		{"file:///etc/passwd", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackOrigin(c.origin); got != c.want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}

func TestValidateNavigateURL(t *testing.T) {
	blocked := []string{
		"file:///etc/passwd",
		"chrome://settings",
		"chrome-extension://abc/popup.html",
		"data:text/html,<script>alert(1)</script>",
		"javascript:alert(1)",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://metadata.google.internal/",
		"http://127.0.0.1/admin",
		"http://10.0.0.5/",
		"http://192.168.1.1/router",
		"http://172.20.0.1/",
		"http://[::1]/",
		"http://localhost/",
		"  ",
		"",
	}
	for _, u := range blocked {
		if err := validateNavigateURL(u); err == nil {
			t.Errorf("expected validateNavigateURL(%q) to fail, got nil", u)
		}
	}

	allowed := []string{
		"https://example.com/",
		"https://api.github.com/repos",
		"http://example.com:8080/page?x=1#anchor",
	}
	for _, u := range allowed {
		if err := validateNavigateURL(u); err != nil {
			t.Errorf("validateNavigateURL(%q) = %v, want nil", u, err)
		}
	}
}
