package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	azip "github.com/yeka/zip"
)

func TestExportSampleRepositoryToEncryptedZip(t *testing.T) {
	repoPath := filepath.Join("test", "psso")
	if _, err := os.Stat(repoPath); err != nil {
		t.Skip("sample repository is not present")
	}

	outPath := filepath.Join(t.TempDir(), "sample.zip")
	const explicitPassword = "test-password"
	if err := run([]string{"svn-fucker", "-zip", "--password", explicitPassword, repoPath, outPath}); err != nil {
		t.Fatal(err)
	}

	zr, err := azip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		t.Fatal("zip has no files")
	}

	var checked bool
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.IsEncrypted() {
			t.Fatalf("%s is not encrypted", file.Name)
		}
		file.SetPassword(explicitPassword)
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open encrypted file %s: %v", file.Name, err)
		}
		_, err = io.Copy(io.Discard, rc)
		closeErr := rc.Close()
		if err != nil {
			t.Fatalf("read encrypted file %s: %v", file.Name, err)
		}
		if closeErr != nil {
			t.Fatalf("close encrypted file %s: %v", file.Name, closeErr)
		}
		checked = true
		break
	}
	if !checked {
		t.Fatal("zip has no regular files")
	}
}

func TestParseArgsDefaultCheckoutMode(t *testing.T) {
	opts, err := parseArgs([]string{"svn-fucker", "./source", "./dest"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.zip {
		t.Fatal("default mode should checkout to a directory, not zip")
	}
	if opts.source != "./source" || opts.dest != "./dest" {
		t.Fatalf("unexpected paths: %#v", opts)
	}
}

func TestParseArgsZipWithPassword(t *testing.T) {
	opts, err := parseArgs([]string{"svn-fucker", "-zip", "./source", "./dest.zip", "--password", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.zip {
		t.Fatal("-zip was not enabled")
	}
	if !opts.passwordSet || opts.password != "secret" {
		t.Fatalf("unexpected password options: %#v", opts)
	}
}

func TestRunRejectsPasswordWithoutZip(t *testing.T) {
	err := run([]string{"svn-fucker", "--password", "secret", "./source", "./dest"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--password requires -zip") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsZipOutputDirectory(t *testing.T) {
	outDir := t.TempDir()
	err := run([]string{"svn-fucker", "-zip", "./missing-source", outDir})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "zip output path must be a file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZipDirWithoutPasswordCreatesPlainZip(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "plain.zip")
	if err := zipDir(srcDir, outPath, ""); err != nil {
		t.Fatal(err)
	}

	zr, err := azip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	if len(zr.File) != 1 {
		t.Fatalf("got %d files, want 1", len(zr.File))
	}
	if zr.File[0].IsEncrypted() {
		t.Fatal("zip should not be encrypted without --password")
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q, want hello", data)
	}
}

func TestZipDirWithPasswordCreatesEncryptedZip(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "encrypted.zip")
	if err := zipDir(srcDir, outPath, "secret"); err != nil {
		t.Fatal(err)
	}

	zr, err := azip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	if len(zr.File) != 1 {
		t.Fatalf("got %d files, want 1", len(zr.File))
	}
	if !zr.File[0].IsEncrypted() {
		t.Fatal("zip should be encrypted with --password")
	}
	zr.File[0].SetPassword("secret")
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, rc)
	closeErr := rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
