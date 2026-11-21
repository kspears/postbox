// Package config reads Postbox's settings from the environment.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// Config is everything `postbox serve` needs to start.
type Config struct {
	IMAPAddr       string
	SMTPAddr       string
	PublicURL      *url.URL
	State          string
	Listen         string
	ClientIPHeader string
	// TrustedProxy is the only peer whose ClientIPHeader is believed.
	TrustedProxy netip.Addr
	CAFile       string
	// PasskeyKey locks the password stored for a passkey that has no PRF output. Without it
	// such passkeys are refused.
	PasskeyKey []byte
}

// DefaultState is the state directory when POSTBOX_STATE is unset.
const DefaultState = "/data/postbox"

// FromEnv builds a Config from getenv (os.Getenv in production). An unset variable and an
// empty one are the same. The error names the variable at fault; main prints it after "error: ".
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		State:          DefaultState,
		Listen:         ":8080",
		ClientIPHeader: getenv("POSTBOX_CLIENT_IP_HEADER"),
		CAFile:         getenv("POSTBOX_CA_FILE"),
	}
	var err error
	if c.IMAPAddr, err = hostPort("POSTBOX_IMAP_ADDR", getenv("POSTBOX_IMAP_ADDR")); err != nil {
		return Config{}, err
	}
	if c.SMTPAddr, err = hostPort("POSTBOX_SMTP_ADDR", getenv("POSTBOX_SMTP_ADDR")); err != nil {
		return Config{}, err
	}
	if c.PublicURL, err = publicURL(getenv("POSTBOX_PUBLIC_URL")); err != nil {
		return Config{}, err
	}
	if v := getenv("POSTBOX_STATE"); v != "" {
		c.State = v
	}
	if v := getenv("POSTBOX_LISTEN"); v != "" {
		if _, port, err := net.SplitHostPort(v); err != nil || port == "" {
			return Config{}, fmt.Errorf("POSTBOX_LISTEN must be [host]:port, got %q", v)
		}
		c.Listen = v
	}
	if v := getenv("POSTBOX_TRUSTED_PROXY"); v != "" {
		if c.TrustedProxy, err = netip.ParseAddr(v); err != nil {
			return Config{}, fmt.Errorf("POSTBOX_TRUSTED_PROXY must be one IP address, the reverse proxy's; a network is not accepted. Got %q", v)
		}
		c.TrustedProxy = c.TrustedProxy.Unmap().WithZone("")
	}
	if c.ClientIPHeader != "" && !c.TrustedProxy.IsValid() {
		return Config{}, fmt.Errorf("POSTBOX_CLIENT_IP_HEADER is set but POSTBOX_TRUSTED_PROXY is not; set it to the reverse proxy's IP address, the only peer whose header is believed")
	}
	if v := getenv("POSTBOX_PASSKEY_KEY"); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(key) != 32 {
			return Config{}, fmt.Errorf("POSTBOX_PASSKEY_KEY must be 32 random bytes in base64, as `openssl rand -base64 32` makes")
		}
		c.PasskeyKey = key
	}
	return c, nil
}

var defaultPort = map[string]string{"http": "80", "https": "443"}

func hostPort(name, v string) (string, error) {
	if v == "" {
		return "", fmt.Errorf("%s is required (host:port)", name)
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("%s must be host:port, got %q", name, v)
	}
	return v, nil
}

// publicURL accepts an https:// origin. Plain http:// is accepted only for localhost and names
// under .localhost, which browsers treat as secure, so local runs and CI need no certificate.
// The result is spelled the way a browser spells an origin (lower-case host, no default port),
// because cookies and passkeys are checked against it.
func publicURL(v string) (*url.URL, error) {
	if v == "" {
		return nil, fmt.Errorf("POSTBOX_PUBLIC_URL is required (https:// URL)")
	}
	malformed := fmt.Errorf("POSTBOX_PUBLIC_URL must be an https:// URL with no path (http:// is accepted only for localhost), got %q", v)
	u, err := url.Parse(v)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, malformed
	}
	if u.Path != "" && u.Path != "/" {
		return nil, malformed
	}
	host := strings.ToLower(u.Hostname())
	local := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil, malformed
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // an IPv6 literal; Hostname() dropped the brackets
	}
	if port := u.Port(); port != "" && port != defaultPort[u.Scheme] {
		host += ":" + port
	}
	return &url.URL{Scheme: u.Scheme, Host: host}, nil
}
