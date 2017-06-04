package main

import (
	"bytes"
	"fmt"
	"net"
	"regexp"

	"github.com/pkg/errors"
)

const defaultRuleName = "default"

// RuleMatcher match an address (IP or domain name) against a set of rules.
type RuleMatcher struct {
	domainMatcher   *domainMatcher
	ipMatcher       *ipMatcher
	ruleToUpstreams map[string][]string

	AllUpstreams []string
}

// NewRuleMatcher creates a RuleMatcher from a given configuration.
func NewRuleMatcher(config map[string]RuleConfig) (*RuleMatcher, error) {
	m := &RuleMatcher{}
	m.ruleToUpstreams = make(map[string][]string)
	domainRules := make(map[string][]string)
	ipRules := make(map[string][]string)

	for name, c := range config {
		if name == defaultRuleName {
			if len(c.Domains) > 0 || len(c.IPs) > 0 {
				return nil, errors.Errorf(
					"default rule '%s' should not have actual rules", name)
			}
		} else {
			domainRules[name] = append([]string{}, c.Domains...)
			ipRules[name] = append([]string{}, c.IPs...)
		}
		m.ruleToUpstreams[name] = append([]string{}, c.Upstreams...)
		m.AllUpstreams = append(m.AllUpstreams, c.Upstreams...)
	}

	var err error
	m.domainMatcher, err = newDomainMatcher(domainRules)
	if err == nil {
		m.ipMatcher, err = newIPMatcher(ipRules)
	}
	return m, err
}

// MatchDomain returns the matching rule and associated upstreams of a domain.
func (m *RuleMatcher) MatchDomain(domain string) (string, []string) {
	rule, matched := m.domainMatcher.Match(domain)
	if matched { // match
		return rule, m.ruleToUpstreams[rule]
	} else if ups, ok := m.ruleToUpstreams[defaultRuleName]; ok { // has default
		return defaultRuleName, ups
	} else { // no default
		return "", nil
	}
}

// MatchIP returns the matching rule and associated upstreams of an IP.
func (m *RuleMatcher) MatchIP(ip net.IP) (string, []string) {
	rule, matched := m.ipMatcher.Match(ip)
	if matched { // match
		return rule, m.ruleToUpstreams[rule]
	} else if ups, ok := m.ruleToUpstreams[defaultRuleName]; ok { // has default
		return defaultRuleName, ups
	} else { // no default
		return "", nil
	}
}

type domainMatcher struct {
	pattern         *regexp.Regexp
	ruleSubmatchIDs map[string]int
}

func newDomainMatcher(rules map[string][]string) (*domainMatcher, error) {
	m := &domainMatcher{}

	if len(rules) == 0 {
		m.pattern = regexp.MustCompile("^$")
		return m, nil
	}

	buf := bytes.NewBufferString("(?i)")
	for name, patterns := range rules {
		if len(patterns) > 0 {
			fmt.Fprintf(buf, "(?P<%s>", name)
			for _, pattern := range patterns {
				fmt.Fprintf(buf, "(^%s$)|", pattern)
			}
			buf.Truncate(buf.Len() - 1)
			buf.WriteString(")|")
		}
	}
	buf.Truncate(buf.Len() - 1)
	var err error
	m.pattern, err = regexp.Compile(buf.String())
	if err != nil {
		return nil, err
	}

	m.ruleSubmatchIDs = make(map[string]int)
	for idx, name := range m.pattern.SubexpNames() {
		if _, isRuleName := rules[name]; isRuleName {
			m.ruleSubmatchIDs[name] = idx
		}
	}
	return m, nil
}

func (m *domainMatcher) Match(domain string) (string, bool) {
	matches := m.pattern.FindStringSubmatchIndex(domain)
	if matches == nil {
		return "", false
	}
	for rule, submatchID := range m.ruleSubmatchIDs {
		if matches[submatchID*2] == 0 {
			return rule, true
		}
	}
	return "", false
}

type ipMatcher struct {
	brt brtNode
}

func newIPMatcher(rules map[string][]string) (*ipMatcher, error) {
	m := &ipMatcher{}
	for name, patterns := range rules {
		for _, pattern := range patterns {
			_, ipNet, err := net.ParseCIDR(pattern)
			if err != nil {
				ip := net.ParseIP(pattern)
				if ip == nil {
					return nil, errors.New(
						"failed to parse ip pattern: " + pattern)
				}
				ipNet = &net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}
			}

			patternLen, bits := ipNet.Mask.Size()
			if bits < 128 {
				patternLen += 128 - bits
			}
			m.brt.Insert(
				bitStrFromBytes(ipNet.IP.To16(), uint(patternLen)), name)
		}
	}
	return m, nil
}

func (m *ipMatcher) Match(ip net.IP) (string, bool) {
	query := bitStrFromBytes(ip.To16(), 128)
	rule, valid := m.brt.FindPrefix(query).(string)
	return rule, valid
}
