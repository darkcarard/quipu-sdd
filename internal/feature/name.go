package feature

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/darkcarard/sdd-framework/internal/state"
)

// Feature names (slugs) are used as directory names (specs/<feature>,
// .sdd/features/<feature>, .sdd/worktrees/<feature>), in the lock file name and
// in the branch sdd/<feature>, so they are restricted to a portable subset:
//
//   - lowercase ASCII letters and digits in words joined by single hyphens;
//   - at most MaxNameLen characters, to keep worktree paths short;
//   - not a reserved word: Windows device names, or CLI subcommands that would
//     make `sdd <command> <feature>` ambiguous.
//
// The human-readable title and description live in state.json (see Describe).

// MaxNameLen bounds feature names.
const MaxNameLen = 40

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedNames cannot be used as feature names.
var reservedNames = []string{
	// Windows device names: such directories cannot be created on Windows.
	"con", "prn", "aux", "nul",
	"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
	"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
	// Subcommands that take a feature argument in the same position.
	"new", "lint", "approve", "chat", "check", "help",
}

// ValidName checks that name is a valid feature name (see the rules above).
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("feature name is empty")
	case len(name) > MaxNameLen:
		return fmt.Errorf("feature name %q is %d characters long; the limit is %d", name, len(name), MaxNameLen)
	case !nameRe.MatchString(name):
		return fmt.Errorf("invalid feature name %q: use lowercase letters and digits in words joined by single hyphens, e.g. user-signup", name)
	case slices.Contains(reservedNames, name):
		return fmt.Errorf("feature name %q is reserved (Windows device name or sdd subcommand)", name)
	}
	return nil
}

// transliterations map common non-ASCII letters (after lowercasing) to ASCII.
var transliterations = map[rune]string{
	'á': "a", 'à': "a", 'ä': "a", 'â': "a", 'ã': "a", 'å': "a", 'ā': "a",
	'é': "e", 'è': "e", 'ë': "e", 'ê': "e", 'ē': "e",
	'í': "i", 'ì': "i", 'ï': "i", 'î': "i", 'ī': "i",
	'ó': "o", 'ò': "o", 'ö': "o", 'ô': "o", 'õ': "o", 'ø': "o", 'ō': "o",
	'ú': "u", 'ù': "u", 'ü': "u", 'û': "u", 'ū': "u",
	'ñ': "n", 'ç': "c", 'ý': "y", 'ÿ': "y",
	'ß': "ss", 'æ': "ae", 'œ': "oe",
}

// Slugify derives a valid feature name from free text, e.g.
// "Registro de Usuarios (v2)!" → "registro-de-usuarios-v2". Accents are
// removed, other symbols become word separators, and a name longer than
// MaxNameLen is cut at a word boundary.
func Slugify(text string) (string, error) {
	var sb strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case transliterations[r] != "":
			sb.WriteString(transliterations[r])
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			// Letters outside the table (e.g. other scripts) are dropped.
		default:
			sb.WriteByte(' ')
		}
	}
	slug := strings.Join(shorten(strings.Fields(sb.String())), "-")
	if slug == "" {
		return "", fmt.Errorf("cannot derive a feature name from %q: pass one with --name", text)
	}
	if err := ValidName(slug); err != nil {
		if slug != text {
			return "", fmt.Errorf("%w, derived from %q: pass another name with --name", err, text)
		}
		return "", fmt.Errorf("%w: pass another name with --name", err)
	}
	return slug, nil
}

// trailingStopwords are dropped from the end of a name that had to be cut, so
// "Integración con el sistema de facturación…" gives "integracion-con-el-sistema"
// rather than "integracion-con-el-sistema-de". Spanish and English.
var trailingStopwords = []string{
	"a", "al", "con", "de", "del", "e", "el", "en", "la", "las", "lo", "los", "o", "para", "por",
	"segun", "sin", "sobre", "u", "un", "una", "unas", "unos", "y",
	"an", "and", "at", "by", "for", "from", "in", "into", "of", "on", "or", "the", "to", "via", "with",
}

// shorten keeps as many leading words as fit in MaxNameLen (joined by hyphens).
// When words had to be dropped, trailing stopwords are dropped too.
func shorten(words []string) []string {
	n, size := 0, 0
	for n < len(words) {
		add := len(words[n])
		if n > 0 {
			add++ // hyphen
		}
		if size+add > MaxNameLen {
			break
		}
		size += add
		n++
	}
	switch {
	case len(words) == 0:
		return nil
	case n == 0: // a single word longer than the limit
		return []string{words[0][:MaxNameLen]}
	case n == len(words):
		return words
	}
	kept := words[:n]
	for len(kept) > 1 && slices.Contains(trailingStopwords, kept[len(kept)-1]) {
		kept = kept[:len(kept)-1]
	}
	return kept
}

// TitleFromName turns a feature name into a title ("user-signup" → "User Signup").
func TitleFromName(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Resolve maps a command argument to an existing feature name. It accepts the
// feature name itself, text that slugifies to it, or its title (case-insensitive),
// so `sdd status "Registro de usuarios"` works as well as `sdd status registro-de-usuarios`.
func Resolve(l state.Layout, arg string) (string, error) {
	names, err := List(l)
	if err != nil {
		return "", err
	}
	if slices.Contains(names, arg) {
		return arg, nil
	}
	if slug, err := Slugify(arg); err == nil && slices.Contains(names, slug) {
		return slug, nil
	}
	for _, n := range names {
		if st, err := l.Load(n); err == nil && st.Title != "" && strings.EqualFold(strings.TrimSpace(st.Title), strings.TrimSpace(arg)) {
			return n, nil
		}
	}
	if err := ValidName(arg); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return "", fmt.Errorf("%w: %s (create it with `sdd spec new %s`)", ErrNotFound, arg, arg)
}
