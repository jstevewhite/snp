package template

import "testing"

func TestExtract(t *testing.T) {
	cases := []struct {
		body string
		want []Var
	}{
		{"echo {{name}}", []Var{{Name: "name"}}},
		{"curl {{url|http://localhost}}", []Var{{Name: "url", Default: strPtr("http://localhost")}}},
		{"{{ name | spaced }}", []Var{{Name: "name", Default: strPtr("spaced")}}},
		{"{{a|1}} ... {{a|2}}", []Var{{Name: "a", Default: strPtr("1")}}},
		{"{{1abc}} {{foo bar}} {{-x}}", nil},
		{"x {{name", nil},
		{"{{a|}}", []Var{{Name: "a"}}},
		{"{{k|{braced}}}", []Var{{Name: "k", Default: strPtr("{braced")}}},
		{"{{b}} {{a}} {{b}}", []Var{{Name: "b"}, {Name: "a"}}},
	}
	for _, tc := range cases {
		got := Extract(tc.body)
		if len(got) != len(tc.want) {
			t.Errorf("Extract(%q) = %+v, want %+v", tc.body, got, tc.want)
			continue
		}
		for i := range got {
			if got[i].Name != tc.want[i].Name || !samePtr(got[i].Default, tc.want[i].Default) {
				t.Errorf("Extract(%q)[%d] = %+v, want %+v", tc.body, i, got[i], tc.want[i])
			}
		}
	}
}

func TestHasVars(t *testing.T) {
	if HasVars("sudo reboot") {
		t.Error("plain text")
	}
	if !HasVars("hello {{who}}") {
		t.Error("variable")
	}
	if HasVars("{{1abc}}") {
		t.Error("invalid only")
	}
}

func TestFill(t *testing.T) {
	cases := []struct {
		body string
		vals map[string]string
		want string
	}{
		{"echo {{name}}", map[string]string{"name": "world"}, "echo world"},
		{"curl {{url|http://x}}", map[string]string{}, "curl http://x"},
		{"curl {{url|http://x}}", map[string]string{"url": ""}, "curl "},
		{"a {{name}} b", map[string]string{}, "a  b"},
		{"{{1abc}}", map[string]string{"1abc": "x"}, "{{1abc}}"},
		{"{{a}}-{{b}}-{{c}}", map[string]string{"a": "1", "b": "2", "c": "3"}, "1-2-3"},
		{"x {{k|{braced}}} y", map[string]string{}, "x {braced} y"},
		{"a {{b c", map[string]string{}, "a {{b c"},
		{`json: {"a": 1}`, map[string]string{}, `json: {"a": 1}`},
		{"{{oops then {{real}}", map[string]string{"real": "v"}, "{{oops then v"},
		{"{{a|1}}-{{a|2}}", map[string]string{}, "1-2"},
		{"{{a|1}}-{{a|2}}", map[string]string{"a": ""}, "-"},
		{"{{a|1}}-{{a|2}}", map[string]string{"a": "z"}, "z-z"},
	}
	for _, tc := range cases {
		if got := Fill(tc.body, tc.vals); got != tc.want {
			t.Errorf("Fill(%q, %v) = %q, want %q", tc.body, tc.vals, got, tc.want)
		}
	}
}

func TestInitial(t *testing.T) {
	def := "inline"
	v := Var{Name: "host", Default: &def}
	if got := Initial(v, map[string]string{"host": "saved"}); got != "saved" {
		t.Errorf("saved = %q", got)
	}
	if got := Initial(v, map[string]string{"host": ""}); got != "inline" {
		t.Errorf("blank saved = %q", got)
	}
	if got := Initial(v, nil); got != "inline" {
		t.Errorf("no saved = %q", got)
	}
	if got := Initial(Var{Name: "host"}, nil); got != "" {
		t.Errorf("no default = %q", got)
	}
}

func strPtr(s string) *string { return &s }

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
