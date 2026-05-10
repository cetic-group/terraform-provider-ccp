package registryuser

import (
	"regexp"
	"strings"
)

// usernamePattern restricts registry usernames to lowercase letters,
// digits and hyphens — same shape as the one cesanta/docker_auth's JWT
// expects, and stricter than DNS so we never end up with edge cases.
func usernamePattern() *regexp.Regexp {
	return regexp.MustCompile(`^[a-z0-9-]+$`)
}

// splitImportID splits a `<registry_id>/<user_id>` import string. Returns
// nil if the format is wrong.
func splitImportID(s string) []string {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	return parts
}
