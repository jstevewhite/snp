// Package portable protects complete export/backup files using the interoperable
// age passphrase format. It is independent of the database encryption key.
package portable

import (
	"errors"
	"io"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/armor"
)

const ArmorHeader = "-----BEGIN AGE ENCRYPTED FILE-----"

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return errors.New("Use a password of at least 12 characters (at most 1024 bytes)")
	}
	return nil
}

// Encrypt writes ASCII-armored age data, with fresh salt and file key each time.
func Encrypt(dst io.Writer, src io.Reader, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return err
	}
	armored := armor.NewWriter(dst)
	encrypted, err := age.Encrypt(armored, recipient)
	if err != nil {
		return err
	}
	if _, err := io.Copy(encrypted, src); err != nil {
		return err
	}
	if err := encrypted.Close(); err != nil {
		return err
	}
	return armored.Close()
}

// Decrypt authenticates the complete stream. Callers must stage output privately
// and only publish it after success: a late authentication error invalidates it.
func Decrypt(dst io.Writer, src io.Reader, password string) error {
	if password == "" || len(password) > 1024 {
		return errors.New("Enter the file password")
	}
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return err
	}
	// Match our writer's default. An untrusted file cannot demand unbounded RAM.
	identity.SetMaxWorkFactor(18)
	decrypted, err := age.Decrypt(armor.NewReader(src), identity)
	if err != nil {
		return errors.New("Cannot unlock file: wrong password, damaged file or unsupported encryption")
	}
	if _, err := io.Copy(dst, decrypted); err != nil {
		return errors.New("Cannot unlock file: damaged, incomplete or too large")
	}
	return nil
}
