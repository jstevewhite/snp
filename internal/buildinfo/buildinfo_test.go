package buildinfo

import "testing"

func TestStringDefaultsToDev(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })

	Version = ""
	if got := String(); got != "dev" {
		t.Fatalf("String() with empty Version = %q, want %q", got, "dev")
	}
}

func TestStringReturnsLinkedVersion(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })

	Version = "v1.2.3"
	if got := String(); got != "v1.2.3" {
		t.Fatalf("String() = %q, want %q", got, "v1.2.3")
	}
}
