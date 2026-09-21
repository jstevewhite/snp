package portable

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/armor"
)

func TestProtectedFiles(t *testing.T) {
	const password = "a long unique passphrase 🗝"
	// Multiple chunks exercise authentication beyond the initial header.
	plain := bytes.Repeat([]byte("sensitive snippet content\n"), 6000)
	var first, second bytes.Buffer
	for _, dst := range []*bytes.Buffer{&first, &second} {
		if err := Encrypt(dst, bytes.NewReader(plain), password); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(dst.Bytes(), []byte("sensitive snippet")) {
			t.Fatal("plaintext leaked")
		}
	}
	if bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("encryption did not randomize")
	}
	var got bytes.Buffer
	if err := Decrypt(&got, bytes.NewReader(first.Bytes()), password); err != nil || !bytes.Equal(got.Bytes(), plain) {
		t.Fatalf("round trip: %v", err)
	}
	// Standard age APIs can recover snp output without any snp metadata or key.
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		t.Fatal(err)
	}
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(first.Bytes())), id)
	if err != nil {
		t.Fatal(err)
	}
	got.Reset()
	if _, err := got.ReadFrom(r); err != nil || !bytes.Equal(got.Bytes(), plain) {
		t.Fatalf("age interoperability: %v", err)
	}
	raw, err := io.ReadAll(armor.NewReader(bytes.NewReader(first.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-1] ^= 1
	excessive := bytes.Replace(raw, []byte(" 18\n"), []byte(" 30\n"), 1)
	if bytes.Equal(raw, excessive) {
		t.Fatal("work factor header not found")
	}
	wrap := func(data []byte) string {
		var b bytes.Buffer
		a := armor.NewWriter(&b)
		if _, err := a.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for name, input := range map[string]string{"wrong password": first.String(), "truncated": first.String()[:first.Len()-100], "tampered payload": wrap(tampered), "excessive work": wrap(excessive)} {
		t.Run(name, func(t *testing.T) {
			pw := password
			if name == "wrong password" {
				pw = "incorrect password"
			}
			var out bytes.Buffer
			if err := Decrypt(&out, strings.NewReader(input), pw); err == nil {
				t.Fatal("invalid file/password accepted")
			}
		})
	}
	if err := Encrypt(&got, bytes.NewReader(plain), "short"); err == nil {
		t.Fatal("short password accepted")
	}
}
