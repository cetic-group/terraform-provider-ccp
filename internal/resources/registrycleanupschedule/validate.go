package registrycleanupschedule

// Plan-time checks of a cleanup schedule, written to accept EXACTLY what the
// platform accepts (cetic-cloud-platform#1984):
//
//   - `app/schemas/registry_retention.py` — `CleanupScheduleCreate` field
//     constraints (Pydantic `min_length`/`max_length` on the RAW value), the
//     name / pattern / cron field validators, `_valider_critere`;
//   - `app/services/registry_cleanup.py` — `NOM_RE`, `MOTIF_DEPOT_RE`,
//     `MOTIF_TAG_RE`, `valider_cron`, `motif_universel`.
//
// The platform strips (`str.strip()`) names and patterns and collapses the
// whitespace of a cron (`" ".join(expr.split())`) BEFORE matching them. Those
// are Python whitespace rules over Unicode text: wherever a value holds a
// non-ASCII character, the check is not reproduced here (Python and Go
// disagree on some whitespace and on length in code points) and the API's
// own answer is relayed instead. A deferred check never rejects: the provider
// is never stricter than the platform, only silent where it cannot be exact.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	critereAll       = "all"
	critereOlderThan = "older_than_days"
	critereKeepLastN = "keep_last_n"

	nameMaxLen       = 64 // Field(max_length=64), raw value
	cronMaxLen       = 64 // Field(max_length=64), raw value, then again after collapse
	patternsMin      = 1  // Field(min_length=1) on the lists
	patternsMax      = 10 // Field(max_length=10) on the lists
	olderMin         = 1
	olderMax         = 3650
	keepMin          = 1
	keepMax          = 1000
	minDaysOnAll     = 7 // JOURS_MIN_SUR_TOUT
	repoPatternLimit = 255
	tagPatternLimit  = 128
)

var (
	// NOM_RE
	nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)
	// MOTIF_DEPOT_RE
	repoPatternRE = regexp.MustCompile(`^[a-z0-9._/*?-]{1,255}$`)
	// MOTIF_TAG_RE
	tagPatternRE = regexp.MustCompile(`^[A-Za-z0-9_.*?-]{1,128}$`)
	// _JETON_CRON (input is ASCII here: `\d` is then [0-9] in both engines)
	cronTokenRE = regexp.MustCompile(`^(\*|\*/\d{1,2}|\d{1,2}(-\d{1,2})?)$`)
)

// _BORNES_CRON
var cronBounds = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}

// isPySpace mirrors Python's `str.isspace()` on one character. ASCII is exact
// (Python also counts the separators U+001C..U+001F); beyond ASCII it is an
// approximation, used only to keep a user's spelling in state (see
// `keepIfSame`), never to accept or reject a value.
func isPySpace(r rune) bool {
	if r < 0x80 {
		return r == ' ' || (r >= '\t' && r <= '\r') || (r >= 0x1c && r <= 0x1f)
	}
	return unicode.IsSpace(r)
}

// pyStrip mirrors `str.strip()`.
func pyStrip(s string) string { return strings.TrimFunc(s, isPySpace) }

// pyFields mirrors `str.split()` (no argument).
func pyFields(s string) []string { return strings.FieldsFunc(s, isPySpace) }

// normalizeCron mirrors the platform: blank → no cron (nil), otherwise the
// whitespace-collapsed expression.
func normalizeCron(s string) *string {
	f := pyFields(s)
	if len(f) == 0 {
		return nil
	}
	out := strings.Join(f, " ")
	return &out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// checkName returns the API's refusal of a name, or "" (valid, or not
// decidable here).
func checkName(raw string) string {
	if !isASCII(raw) {
		return ""
	}
	if len(raw) < 1 || len(raw) > nameMaxLen {
		return fmt.Sprintf("must be 1 to %d characters long.", nameMaxLen)
	}
	if !nameRE.MatchString(pyStrip(raw)) {
		return "invalid name: 1 to 64 characters, starting with a letter or a digit, " +
			"then letters, digits, spaces and \". _ -\" (leading and trailing spaces are removed)."
	}
	return ""
}

// patternKind selects the repository or the tag rules.
type patternKind int

const (
	repoKind patternKind = iota
	tagKind
)

// checkPattern returns the API's refusal of one pattern, or "".
func checkPattern(kind patternKind, raw string) string {
	if !isASCII(raw) {
		return ""
	}
	m := pyStrip(raw)
	if kind == repoKind {
		if !repoPatternRE.MatchString(m) || strings.Contains(m, "**") {
			return fmt.Sprintf("invalid repository pattern %q: lowercase letters, digits, \". _ - /\" "+
				"and the wildcards \"*\" \"?\" (no \"**\"), %d characters at most.", m, repoPatternLimit)
		}
		return ""
	}
	if !tagPatternRE.MatchString(m) || strings.Contains(m, "**") {
		return fmt.Sprintf("invalid tag pattern %q: letters, digits, \". _ -\" "+
			"and the wildcards \"*\" \"?\" (no \"**\"), %d characters at most.", m, tagPatternLimit)
	}
	return ""
}

// patternElem is one list element as seen at plan time.
type patternElem struct {
	Value   string
	Null    bool
	Unknown bool
}

// checkPatterns returns the refusals of a whole list (count, each element,
// duplicates after stripping). Elements that are unknown, or not decidable
// here, are left to the API.
func checkPatterns(kind patternKind, elems []patternElem) []string {
	var out []string
	if len(elems) < patternsMin || len(elems) > patternsMax {
		return []string{fmt.Sprintf("must hold %d to %d patterns.", patternsMin, patternsMax)}
	}
	seen := map[string]bool{}
	for _, e := range elems {
		switch {
		case e.Unknown:
			continue
		case e.Null:
			out = append(out, "patterns must not be null.")
			continue
		}
		if msg := checkPattern(kind, e.Value); msg != "" {
			out = append(out, msg)
			continue
		}
		if !isASCII(e.Value) {
			continue
		}
		m := pyStrip(e.Value)
		if seen[m] {
			out = append(out, fmt.Sprintf("duplicate pattern %q.", m))
		}
		seen[m] = true
	}
	return out
}

// tri is a three-valued boolean: a plan-time fact may be undecidable.
type tri int

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

// isUniversal mirrors `motif_universel`: no literal character and at least
// one `*` — on a repository pattern `/` is not a literal either.
func isUniversal(m string, repo bool) bool {
	if m == "" || !strings.Contains(m, "*") {
		return false
	}
	for _, r := range m {
		if r == '*' || r == '?' || (repo && r == '/') {
			continue
		}
		return false
	}
	return true
}

// anyUniversal mirrors `any(motif_universel(…) for …)` over the stripped
// patterns. One universal pattern decides; otherwise an undecidable element
// (unknown, null or non-ASCII) leaves the answer unknown.
func anyUniversal(elems []patternElem, repo bool, listUnknown bool) tri {
	if listUnknown {
		return triUnknown
	}
	res := triFalse
	for _, e := range elems {
		if e.Unknown || e.Null || !isASCII(e.Value) {
			res = triUnknown
			continue
		}
		if isUniversal(pyStrip(e.Value), repo) {
			return triTrue
		}
	}
	return res
}

func coversEverything(repos, tags tri) tri {
	if repos == triFalse || tags == triFalse {
		return triFalse
	}
	if repos == triTrue && tags == triTrue {
		return triTrue
	}
	return triUnknown
}

// checkCriterion mirrors `_valider_critere`. `value` is nil when null;
// `valueUnknown` skips every check that needs it; `everything` is the
// "patterns cover the whole registry" fact.
func checkCriterion(critere string, value *int64, valueUnknown bool, everything tri) string {
	switch critere {
	case critereAll:
		// An unknown value may still resolve to null: only a known one is judged.
		if !valueUnknown && value != nil {
			return "criterion \"all\" takes no value: remove `value`."
		}
		if everything == triTrue {
			return "criterion \"all\" on patterns that match every repository and every tag " +
				"(\"*\", \"?*\", \"*/*\"…) would empty the whole registry: restrict one of the patterns."
		}
	case critereOlderThan:
		if valueUnknown {
			return ""
		}
		if value == nil || *value < olderMin || *value > olderMax {
			return fmt.Sprintf("criterion \"older_than_days\" requires a `value` from %d to %d days.", olderMin, olderMax)
		}
		if everything == triTrue && *value < minDaysOnAll {
			return fmt.Sprintf("on every repository and every tag, \"older_than_days\" requires at least %d days "+
				"(otherwise the whole registry would be emptied): raise `value` or restrict one of the patterns.", minDaysOnAll)
		}
	case critereKeepLastN:
		if valueUnknown {
			return ""
		}
		if value == nil || *value < keepMin || *value > keepMax {
			return fmt.Sprintf("criterion \"keep_last_n\" requires a `value` from %d to %d.", keepMin, keepMax)
		}
	}
	return ""
}

// checkCron mirrors the `cron` field: raw length, blank = manual, then
// `valider_cron` on the collapsed expression.
func checkCron(raw string) string {
	if !isASCII(raw) {
		return ""
	}
	if len(raw) > cronMaxLen {
		return fmt.Sprintf("must be %d characters at most.", cronMaxLen)
	}
	champs := pyFields(raw)
	if len(champs) == 0 {
		return "" // blank: manual schedule, like null
	}
	expr := strings.Join(champs, " ")
	if len(champs) != 5 || len(expr) > cronMaxLen {
		return "invalid cron: 5 fields expected (minute hour day-of-month month day-of-week)."
	}
	if champs[2] != "*" && champs[4] != "*" {
		return "invalid cron: day of month and day of week cannot both be restricted " +
			"(leave one of them at \"*\")."
	}
	for i, champ := range champs {
		lo, hi := cronBounds[i][0], cronBounds[i][1]
		jetons := strings.Split(champ, ",")
		if len(jetons) > 1 {
			for _, j := range jetons {
				if j == "*" {
					return fmt.Sprintf("invalid cron: \"*\" inside a list (%q) is not supported.", champ)
				}
			}
		}
		for _, j := range jetons {
			if !cronTokenRE.MatchString(j) {
				return fmt.Sprintf("invalid cron: %q is not supported.", j)
			}
			if strings.HasPrefix(j, "*/") {
				if n, _ := strconv.Atoi(j[2:]); n < 1 {
					return "invalid cron: zero step."
				}
				continue
			}
			if j == "*" {
				continue
			}
			parts := strings.Split(j, "-")
			prev := -1
			for _, p := range parts {
				b, _ := strconv.Atoi(p)
				if b < lo || b > hi || b < prev {
					return fmt.Sprintf("invalid cron: %q out of bounds [%d-%d].", j, lo, hi)
				}
				prev = b
			}
		}
	}
	return ""
}
