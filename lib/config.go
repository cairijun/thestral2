package lib

import (
	"io/ioutil"

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
	HandshakeTimeout string   `yaml:"handshake_timeout"`
}

// KCPConfig contains configuration about the KCP protocol.
type KCPConfig struct {
	Mode     string `yaml:"mode"`
	Optimize string `yaml:"optimize"`
	FEC      bool   `yaml:"fec"`
	FECDist  string `yaml:"fec_dist"`
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
func ParseConfigFile(configFile string) (*Config, error) {
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
