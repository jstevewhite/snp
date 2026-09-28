// Package template parses snp's {{name}} / {{name|default}} placeholders.
// The grammar matches web/src/lib/templates.ts (spec §4). The shell picker
// fills from the values the user left in the form: a present value is used
// as written, including when it is empty.
package template

import (
	"regexp"
	"strings"
)

// Var is one distinct placeholder, in order of first appearance.
// Default is nil when that first placeholder has no inline default.
type Var struct {
	Name    string
	Default *string
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type placeholder struct {
	close int
	name  string
	def   *string
	valid bool
}

// findPlaceholder scans from a '{{'. ok is false when the placeholder is
// unclosed. An invalid name is ok with valid false; the caller then resumes
// just after the opening braces so a real placeholder nested inside is found.
func findPlaceholder(body string, open int) (placeholder, bool) {
	rest := body[open+2:]
	rel := strings.Index(rest, "}}")
	if rel == -1 {
		return placeholder{}, false
	}
	close := open + 2 + rel
	inner := body[open+2 : close]
	rawName := inner
	var def *string
	if pipe := strings.Index(inner, "|"); pipe != -1 {
		rawName = inner[:pipe]
		d := strings.TrimSpace(inner[pipe+1:])
		if d != "" {
			def = &d
		}
	}
	rawName = strings.TrimSpace(rawName)
	if !nameRe.MatchString(rawName) {
		return placeholder{close: close}, true
	}
	return placeholder{close: close, name: rawName, def: def, valid: true}, true
}

// Extract lists distinct variables in order of first appearance.
// Invalid and unclosed placeholders are ignored. The first occurrence of a
// name supplies its default.
func Extract(body string) []Var {
	var vars []Var
	seen := map[string]bool{}
	for i := 0; i < len(body); {
		rel := strings.Index(body[i:], "{{")
		if rel == -1 {
			break
		}
		open := i + rel
		ph, ok := findPlaceholder(body, open)
		if !ok {
			break
		}
		if ph.valid && !seen[ph.name] {
			seen[ph.name] = true
			vars = append(vars, Var{Name: ph.name, Default: ph.def})
		}
		if ph.valid {
			i = ph.close + 2
		} else {
			i = open + 2
		}
	}
	return vars
}

// HasVars reports whether body contains at least one valid placeholder.
func HasVars(body string) bool { return len(Extract(body)) > 0 }

// Initial is what a form box shows before editing: the saved default when
// it is non-empty, otherwise the inline default, otherwise empty.
func Initial(v Var, saved map[string]string) string {
	if s, ok := saved[v.Name]; ok && s != "" {
		return s
	}
	if v.Default != nil {
		return *v.Default
	}
	return ""
}

// Fill substitutes placeholders. A name present in values is used as-is,
// including the empty string, at every occurrence. A missing name falls
// back to that occurrence's own inline default, then to empty. Invalid
// placeholders are left as literal text.
func Fill(body string, values map[string]string) string {
	var out strings.Builder
	for i := 0; i < len(body); {
		rel := strings.Index(body[i:], "{{")
		if rel == -1 {
			out.WriteString(body[i:])
			break
		}
		open := i + rel
		out.WriteString(body[i:open])
		ph, ok := findPlaceholder(body, open)
		if !ok {
			out.WriteString(body[open:])
			break
		}
		if !ph.valid {
			out.WriteString("{{")
			i = open + 2
			continue
		}
		if val, ok := values[ph.name]; ok {
			out.WriteString(val)
		} else if ph.def != nil {
			out.WriteString(*ph.def)
		}
		i = ph.close + 2
	}
	return out.String()
}
