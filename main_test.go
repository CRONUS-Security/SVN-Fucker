package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	azip "github.com/yeka/zip"
)

func TestExportSampleRepositoryToEncryptedZip(t *testing.T) {
	repoPath := filepath.Join("test", "psso")
	if _, err := os.Stat(repoPath); err != nil {
		t.Skip("sample repository is not present")
	}

	outPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := run([]string{"svn-fucker", repoPath, outPath}); err != nil {
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
		file.SetPassword(zipPassword)
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
