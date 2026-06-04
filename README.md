# SVN Export Zipper

Pure CLI tool for exporting the latest revision from an FSFS SVN repository and writing a password-protected zip.

## Usage

```powershell
svn-fucker.exe ./test/psso ./test.zip
```

The zip password is fixed to:

```text
svn12345
```

The tool reads the repository directly, writes exported files into a temporary directory, creates the zip, and removes the temporary directory before exiting.

## Build

Windows:

```powershell
go build -o svn-fucker.exe .
```

Linux static binary:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o svn-fucker .
```

With `CGO_ENABLED=0`, the Linux binary does not depend on a specific glibc version.
