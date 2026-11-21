package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func required() map[string]string {
	return map[string]string{
		"POSTBOX_IMAP_ADDR":  "mail.example.test:993",
		"POSTBOX_SMTP_ADDR":  "mail.example.test:465",
		"POSTBOX_PUBLIC_URL": "https://mail.example.test",
	}
}

func TestFromEnvDefaults(t *testing.T) {
	c, err := FromEnv(env(required()))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "/data/postbox" || c.Listen != ":8080" {
		t.Errorf("defaults: state %q, listen %q", c.State, c.Listen)
	}
	if c.IMAPAddr != "mail.example.test:993" || c.SMTPAddr != "mail.example.test:465" {
		t.Errorf("addresses: imap %q, smtp %q", c.IMAPAddr, c.SMTPAddr)
	}
	if c.PublicURL.String() != "https://mail.example.test" {
		t.Errorf("public URL: %q", c.PublicURL)
	}
	if c.ClientIPHeader != "" || c.CAFile != "" {
		t.Errorf("optional settings not empty: %q, %q", c.ClientIPHeader, c.CAFile)
	}
}

func TestFromEnvOptional(t *testing.T) {
	m := required()
	m["POSTBOX_STATE"] = "/tmp/state"
	m["POSTBOX_LISTEN"] = "127.0.0.1:9000"
	m["POSTBOX_CLIENT_IP_HEADER"] = "X-Forwarded-For"
	m["POSTBOX_TRUSTED_PROXY"] = "172.20.0.2"
	m["POSTBOX_CA_FILE"] = "/ca.crt"
	m["POSTBOX_PASSKEY_KEY"] = strings.Repeat("A", 43) + "="
	c, err := FromEnv(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "/tmp/state" || c.Listen != "127.0.0.1:9000" || c.ClientIPHeader != "X-Forwarded-For" || c.TrustedProxy.String() != "172.20.0.2" || c.CAFile != "/ca.crt" || len(c.PasskeyKey) != 32 {
		t.Errorf("got %+v", c)
	}
}

func TestFromEnvErrors(t *testing.T) {
	tests := []struct {
		name, key, value, want string
	}{
		{"imap unset", "POSTBOX_IMAP_ADDR", "", "POSTBOX_IMAP_ADDR is required (host:port)"},
		{"smtp unset", "POSTBOX_SMTP_ADDR", "", "POSTBOX_SMTP_ADDR is required (host:port)"},
		{"url unset", "POSTBOX_PUBLIC_URL", "", "POSTBOX_PUBLIC_URL is required (https:// URL)"},
		{"imap no port", "POSTBOX_IMAP_ADDR", "mail.example.test", `POSTBOX_IMAP_ADDR must be host:port, got "mail.example.test"`},
		{"imap no host", "POSTBOX_IMAP_ADDR", ":993", `POSTBOX_IMAP_ADDR must be host:port, got ":993"`},
		{"smtp no port", "POSTBOX_SMTP_ADDR", "mail.example.test:", `POSTBOX_SMTP_ADDR must be host:port, got "mail.example.test:"`},
		{"listen", "POSTBOX_LISTEN", "8080", `POSTBOX_LISTEN must be [host]:port, got "8080"`},
		{"listen no port", "POSTBOX_LISTEN", ":", `POSTBOX_LISTEN must be [host]:port, got ":"`},
		{"url http", "POSTBOX_PUBLIC_URL", "http://mail.example.test", "POSTBOX_PUBLIC_URL must be an https:// URL"},
		{"url lookalike", "POSTBOX_PUBLIC_URL", "http://notlocalhost", "POSTBOX_PUBLIC_URL must be an https:// URL"},
		{"url path", "POSTBOX_PUBLIC_URL", "https://mail.example.test/mail", "POSTBOX_PUBLIC_URL must be an https:// URL"},
		{"header without proxy", "POSTBOX_CLIENT_IP_HEADER", "X-Forwarded-For", "POSTBOX_CLIENT_IP_HEADER is set but POSTBOX_TRUSTED_PROXY is not"},
		{"proxy a network", "POSTBOX_TRUSTED_PROXY", "172.20.0.0/16", "POSTBOX_TRUSTED_PROXY must be one IP address"},
		{"passkey key short", "POSTBOX_PASSKEY_KEY", "c2hvcnQ=", "POSTBOX_PASSKEY_KEY must be 32 random bytes in base64"},
		{"passkey key not base64", "POSTBOX_PASSKEY_KEY", "not base64!", "POSTBOX_PASSKEY_KEY must be 32 random bytes in base64"},
		{"url no scheme", "POSTBOX_PUBLIC_URL", "mail.example.test", "POSTBOX_PUBLIC_URL must be an https:// URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := required()
			m[tt.key] = tt.value
			_, err := FromEnv(env(m))
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("got %v, want prefix %q", err, tt.want)
			}
		})
	}
}

func TestPublicURLIsABrowserOrigin(t *testing.T) {
	tests := map[string]string{
		"http://localhost":               "http://localhost",
		"http://localhost:8080":          "http://localhost:8080",
		"http://LOCALHOST:8026":          "http://localhost:8026",
		"http://postbox.localhost:80":    "http://postbox.localhost",
		"https://mail.example.test/":     "https://mail.example.test",
		"https://Mail.Example.Test":      "https://mail.example.test",
		"https://mail.example.test:443":  "https://mail.example.test",
		"https://mail.example.test:8443": "https://mail.example.test:8443",
		"https://mail.example.test?":     "https://mail.example.test",
		"https://[::1]:8443":             "https://[::1]:8443",
		"https://[::1]":                  "https://[::1]",
	}
	for v, want := range tests {
		m := required()
		m["POSTBOX_PUBLIC_URL"] = v
		c, err := FromEnv(env(m))
		if err != nil {
			t.Errorf("%s: %v", v, err)
			continue
		}
		if got := c.PublicURL.String(); got != want {
			t.Errorf("%s: got %q, want %q", v, got, want)
		}
	}
}
