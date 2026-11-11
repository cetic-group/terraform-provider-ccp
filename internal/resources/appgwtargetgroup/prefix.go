package appgwtargetgroup

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const bucketPrefixMax = 512

var bucketPrefixChars = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// canonicalBucketPrefix applies the platform's rules to a folder name and
// returns its canonical form (no leading or trailing slash, no empty segment).
func canonicalBucketPrefix(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > bucketPrefixMax {
		return "", fmt.Errorf("the folder is too long (%d characters at most)", bucketPrefixMax)
	}
	if !bucketPrefixChars.MatchString(value) {
		return "", fmt.Errorf("the folder may only contain letters, digits, dots, hyphens, underscores and slashes")
	}
	var segments []string
	for _, s := range strings.Split(value, "/") {
		if s == "" {
			continue
		}
		if s == "." || s == ".." {
			return "", fmt.Errorf("the folder cannot contain a \".\" or \"..\" segment")
		}
		segments = append(segments, s)
	}
	return strings.Join(segments, "/"), nil
}

// bucketPrefixValidator checks the folder like the platform does, and asks for
// the canonical spelling so the planned value always equals the stored one.
type bucketPrefixValidator struct{}

func (bucketPrefixValidator) Description(context.Context) string {
	return "must be a canonical bucket folder such as `site/assets`"
}

func (v bucketPrefixValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (bucketPrefixValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	canon, err := canonicalBucketPrefix(got)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid bucket folder", err.Error()+".")
		return
	}
	if canon != got {
		resp.Diagnostics.AddAttributeError(req.Path, "Bucket folder is not in canonical form",
			fmt.Sprintf("Write the folder as %q (no leading or trailing slash, no repeated slash, no surrounding space).", canon))
	}
}
