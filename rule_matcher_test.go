package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
)

var domainRules = map[string][]string{
	"r1": []string{`some\.domain.name`, `yyy\.xxx`},
	"r2": []string{`.*\.domain`},
	"r3": []string{`some(\.other)?\.(cn|com)`},
}

var domainQueries = [][2]string{
	{"some.domain.name", "r1"},
	{"yyy.xxx", "r1"},
	{"some.domain.name.com", ""},
	{"another.domain", "r2"},
	{"another.DOMAIN", "r2"},
	{"some.other.cn", "r3"},
	{"some.COM", "r3"},
	{"host.some.other.cn", ""},
}

var ipRules = map[string][]string{
	"r1": []string{"192.168.0.0/16"},
	"r2": []string{"192.168.0.0/24"},
	"r3": []string{"192.168.1.1", "192.168.2.0/24"},
	"r4": []string{"2001:db8::/48"},
	"r5": []string{"c0a8::/16"},
	"r6": []string{"::1", "0::abcd:1"},
}

var ipQueries = [][2]string{
	{"192.168.1.2", "r1"},
	{"192.168.0.1", "r2"},
	{"192.168.1.1", "r3"},
	{"192.168.2.1", "r3"},
	{"172.18.18.1", ""},
	{"2001:db8:f::1", ""},
	{"2001:db8::abcd", "r4"},
	{"c0a8::1", "r5"},
	{"::1", "r6"},
	{"0::abcd:1", "r6"},
}

var config map[string]RuleConfig

func init() {
	cfg := make(map[string]*RuleConfig)
	for name, rule := range domainRules {
		if _, exists := config[name]; !exists {
			cfg[name] = &RuleConfig{Upstreams: []string{name + "Ups"}}
		}
		cfg[name].Domains = append(cfg[name].Domains, rule...)
	}
	for name, rule := range ipRules {
		if _, exists := cfg[name]; !exists {
			cfg[name] = &RuleConfig{Upstreams: []string{name + "Ups"}}
		}
		cfg[name].IPs = append(cfg[name].IPs, rule...)
	}

	config = make(map[string]RuleConfig)
	for k, v := range cfg {
		config[k] = *v
	}
	config["default"] = RuleConfig{Upstreams: []string{"defaultUps"}}
}

func TestDomainMatcher(t *testing.T) {
	m, err := newDomainMatcher(domainRules)
	require.NoError(t, err)
	for _, q := range domainQueries {
		rule, matched := m.Match(q[0])
		if q[1] == "" {
			assert.False(t, matched, "%s should not be matched: %s", q[0], rule)
		} else {
			assert.True(t, matched)
			assert.Equal(
				t, q[1], rule,
				"%s not matched, expected %s got %s", q[0], q[1], rule)
		}
	}
}

func TestIPMatcher(t *testing.T) {
	m, err := newIPMatcher(ipRules)
	require.NoError(t, err)
	for _, q := range ipQueries {
		rule, matched := m.Match(net.ParseIP(q[0]))
		if q[1] == "" {
			assert.False(t, matched, "%s should not be matched: %s", q[0], rule)
		} else {
			assert.True(t, matched)
			assert.Equal(
				t, q[1], rule,
				"%s not matched, expected %s got %s", q[0], q[1], rule)
		}
	}
}

func TestRuleMatcher(t *testing.T) {
	m, err := NewRuleMatcher(config)
	require.NoError(t, err)

	assert.Len(t, m.AllUpstreams, 7)
	assert.Subset(t,
		[]string{
			"r1Ups", "r2Ups", "r3Ups", "r4Ups", "r5Ups", "r6Ups", "defaultUps"},
		m.AllUpstreams)

	for _, q := range domainQueries {
		name, upstreams := m.MatchDomain(q[0])
		var exp string
		if q[1] == "" {
			exp = "default"
		} else {
			exp = q[1]
		}
		assert.Equal(t, exp, name)
		assert.Equal(
			t, []string{exp + "Ups"}, upstreams,
			"%s mismatch, expected %s got %s(%v)", q[0], exp, name, upstreams)
	}

	for _, q := range ipQueries {
		name, upstreams := m.MatchIP(net.ParseIP(q[0]))
		var exp string
		if q[1] == "" {
			exp = "default"
		} else {
			exp = q[1]
		}
		assert.Equal(t, exp, name)
		assert.Equal(
			t, []string{exp + "Ups"}, upstreams,
			"%s mismatch, expected %s got %s(%v)", q[0], exp, name, upstreams)
	}
}
