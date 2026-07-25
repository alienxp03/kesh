package catalog

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

type SSHHost struct {
	Name   string
	Target string
}

func ReadSSHHosts(path, defaultUser string) []SSHHost {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ParseSSHHosts(string(content), defaultUser)
}

func ParseSSHHosts(content, defaultUser string) []SSHHost {
	wildcard := regexp.MustCompile(`[*?!]`)
	options := map[string]map[string]string{}
	var current []string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		switch strings.ToLower(parts[0]) {
		case "host":
			current = nil
			for _, host := range parts[1:] {
				if !wildcard.MatchString(host) {
					current = append(current, host)
					if options[host] == nil {
						options[host] = map[string]string{}
					}
				}
			}
		case "user", "hostname", "port":
			if len(parts) < 2 {
				continue
			}
			key := strings.ToLower(parts[0])
			for _, host := range current {
				if options[host][key] == "" {
					options[host][key] = parts[1]
				}
			}
		}
	}
	names := make([]string, 0, len(options))
	for name := range options {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]SSHHost, 0, len(names))
	for _, name := range names {
		hostname := strings.ReplaceAll(options[name]["hostname"], "%h", name)
		if hostname == "" {
			hostname = name
		}
		user := options[name]["user"]
		if user == "" {
			user = defaultUser
		}
		port := options[name]["port"]
		if port == "" {
			port = "22"
		}
		target := hostname + ":" + port
		if user != "" {
			target = user + "@" + target
		}
		result = append(result, SSHHost{Name: name, Target: target})
	}
	return result
}
