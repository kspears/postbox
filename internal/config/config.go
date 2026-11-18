// Package config reads Postbox's settings from the environment.
package config

import (
	"fmt"
	"net"
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
	CAFile         string
}

// FromEnv builds a Config from getenv (os.Getenv in production). An unset variable and an
// empty one are the same. The error names the variable at fault; main prints it after "error: ".
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		State:          "/data/postbox",
		Listen:         ":8080",
		ClientIPHeader: getenv("POSTBOX_CLIENT_IP_HEADER"),
		CAFile:         getenv("POSTBOX_CA_FILE"),
	}
	var err error
	if c.IMAPAddr, err = hostPort("POSTBOX_IMAP_ADDR", getenv); err != nil {
		return Config{}, err
	}
	if c.SMTPAddr, err = hostPort("POSTBOX_SMTP_ADDR", getenv); err != nil {
		return Config{}, err
	}
	if c.PublicURL, err = publicURL(getenv("POSTBOX_PUBLIC_URL")); err != nil {
		return Config{}, err
	}
	if v := getenv("POSTBOX_STATE"); v != "" {
		c.State = v
	}
	if v := getenv("POSTBOX_LISTEN"); v != "" {
		if _, _, err := net.SplitHostPort(v); err != nil {
			return Config{}, fmt.Errorf("POSTBOX_LISTEN must be [host]:port, got %q", v)
		}
		c.Listen = v
	}
	return c, nil
}

func hostPort(name string, getenv func(string) string) (string, error) {
	v := getenv(name)
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
	u.Path = ""
	return u, nil
}
