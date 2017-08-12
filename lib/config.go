package lib

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pkg/errors"
	"github.com/richardtsai/thestral2/db"
	"gopkg.in/yaml.v2"
)

// Config contains the configuration of a thestral service.
type Config struct {
	Downstreams map[string]ProxyConfig `yaml:"downstreams"`
	Upstreams   map[string]ProxyConfig `yaml:"upstreams"`
	Rules       map[string]RuleConfig  `yaml:"rules"`
	Logging     LoggingConfig          `yaml:"logging"`
	DB          *db.Config             `yaml:"db"`
	Misc        MiscConfig             `yaml:"misc"`
}

// ProxyConfig describes a proxy protocol.
type ProxyConfig struct {
	Protocol  string                 `yaml:"protocol"`
	Transport *TransportConfig       `yaml:"transport"`
	Settings  map[string]interface{} `yaml:",inline"`
}

// TransportConfig describes a transport layer.
type TransportConfig struct {
	Compression string       `yaml:"compression"`
	TLS         *TLSConfig   `yaml:"tls"`
	KCP         *KCPConfig   `yaml:"kcp"`
	Proxied     *ProxyConfig `yaml:"proxied"`
}

// TLSConfig contains the TLS configuration on some transport.
type TLSConfig struct {
	Cert             string   `yaml:"cert"`
	Key              string   `yaml:"key"`
	VerifyClient     bool     `yaml:"verify_client"`
	CAs              []string `yaml:"cas"`
	ExtraCAs         []string `yaml:"extra_cas"`
	ClientCAs        []string `yaml:"client_cas"`
	SessionCacheSize int      `yaml:"session_cache_size"`
	HandshakeTimeout string   `yaml:"handshake_timeout"`
}

// KCPConfig contains configuration about the KCP protocol.
type KCPConfig struct {
	Mode              string `yaml:"mode"`
	Optimize          string `yaml:"optimize"`
	FEC               bool   `yaml:"fec"`
	FECDist           string `yaml:"fec_dist"`
	KeepAliveInterval string `yaml:"keep_alive_interval"`
	KeepAliveTimeout  string `yaml:"keep_alive_timeout"`
}

// RuleConfig describes how to dispatch proxy requests.
type RuleConfig struct {
	Upstreams []string `yaml:"upstreams"`
	IPs       []string `yaml:"ips"`
	Domains   []string `yaml:"domains"`
}

// LoggingConfig contains configuration about logging.
type LoggingConfig struct {
	File   string `yaml:"file"`
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MiscConfig contains configuration that doesn't fall into any of above.
type MiscConfig struct {
	ConnectTimeout string `yaml:"connect_timeout"`
	PProfAddr      string `yaml:"pprof_addr"`
}

// ParseConfigFile parses a given configuration file into a Config struct.
// If an empty string is given, the configuration file will be searched
// in some default locations.
func ParseConfigFile(configFile string) (*Config, error) {
	var err error
	if configFile == "" {
		if configFile, err = getDefaultConfigFile(); err != nil {
			return nil, err
		}
	}

	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.UnmarshalStrict(configData, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func getDefaultConfigFile() (string, error) {
	candidates := []string{
		"thestral2.yml",
		filepath.Join(GetHomePath(), ".thestral2.yml"),
	}
	if runtime.GOOS != "windows" {
		candidates = append(candidates,
			"/usr/local/etc/thestral2.yml",
			"/usr/local/etc/thestral2/config.yml",
			"/usr/etc/thestral2.yml",
			"/usr/etc/thestral2/config.yml")
	}
	for _, c := range candidates {
		if s, err := os.Stat(c); err == nil && s.Mode().IsRegular() {
			return c, nil
		}
	}
	return "", errors.New("no config file found in the default locations")
}
