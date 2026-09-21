package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jstevewhite/snp/internal/portable"
	"golang.org/x/term"
)

// Recovery works without opening the database, configuration, or app key.
func runDecrypt(args []string) {
	fs := flag.NewFlagSet("decrypt", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: snp decrypt <input.json.age|input.zip.age> <output.json|output.zip>\nPrompts privately for the file password. Never overwrites an existing file.")
	}
	if err := fs.Parse(args); err != nil {
		if err != flag.ErrHelp {
			os.Exit(2)
		}
		return
	}
	if fs.NArg() != 2 {
		usageError("usage: snp decrypt <encrypted-file> <new-output-file>")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fatal(fmt.Errorf("run snp decrypt in a terminal to enter the password privately"))
	}
	fmt.Fprint(os.Stderr, "File password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatal(err)
	}
	err = decryptFile(fs.Arg(0), fs.Arg(1), string(password))
	clear(password)
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "File unlocked. Keep the decrypted file private. For a backup, stop snp before following RESTORE.txt.")
}

// Stage privately, authenticate through EOF, then publish without clobbering.
func decryptFile(src, dest, password string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("output already exists; choose a new path")
	} else if !os.IsNotExist(err) {
		return err
	}
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	stat, err := input.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("choose a regular encrypted file")
	}
	output, err := os.CreateTemp(filepath.Dir(dest), ".snp-unlock-")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	if err := portable.Decrypt(output, input, password); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	// Link fails atomically if the destination appeared during decryption.
	return os.Link(output.Name(), dest)
}
