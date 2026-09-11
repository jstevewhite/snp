package store

import "testing"

func TestValidTagName(t *testing.T) {
	valid := []string{"ops", "python", "caddy2", "a", "deploy-script"}
	for _, name := range valid {
		if !ValidTagName(name) {
			t.Errorf("ValidTagName(%q) = false, want true", name)
		}
	}
	invalid := []string{"", "Python", "bad tag", "bad_tag", "-lead", "a b", "café"}
	for _, name := range invalid {
		if ValidTagName(name) {
			t.Errorf("ValidTagName(%q) = true, want false", name)
		}
	}
}
