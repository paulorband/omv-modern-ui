// Package config loads runtime configuration from the environment.
package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Listen       string
	OMVURL       string
	DockerSocket string
	DataDir      string
	AppDataDir   string
	CatalogURL   string
	Mock         bool
	PUID         string
	PGID         string
	TZ           string
}

// Load reads OMVHOME_* variables, applying the defaults documented in SPEC-00.
func Load() Config {
	dataDir := env("OMVHOME_DATA_DIR", "/var/lib/omv-home")
	return Config{
		Listen:       env("OMVHOME_LISTEN", ":8080"),
		OMVURL:       env("OMVHOME_OMV_URL", "http://127.0.0.1"),
		DockerSocket: env("OMVHOME_DOCKER_SOCKET", "/var/run/docker.sock"),
		DataDir:      dataDir,
		AppDataDir:   env("OMVHOME_APPDATA_DIR", filepath.Join(dataDir, "appdata")),
		CatalogURL:   env("OMVHOME_CATALOG_URL", ""),
		Mock:         envBool("OMVHOME_MOCK", false),
		PUID:         env("OMVHOME_PUID", "1000"),
		PGID:         env("OMVHOME_PGID", "100"),
		TZ:           env("OMVHOME_TZ", systemTimezone()),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return def
	}
	return v
}

// systemTimezone returns $TZ, then /etc/timezone, then UTC.
func systemTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	return "UTC"
}

// OMVPort is the TCP port of the OMV web interface, taken from OMVURL (80/443 when implicit).
// The UI uses it to build the "advanced settings" link.
func (c Config) OMVPort() int {
	u, err := url.Parse(c.OMVURL)
	if err != nil {
		return 80
	}
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 && p < 65536 {
		return p
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}
