# SVN Exporter

Pure CLI tool for exporting the latest revision from an FSFS SVN repository into a directory or zip file.

## Usage

Checkout/export the latest revision into a directory:

```powershell
svn-fucker.exe ./test/psso ./out
```

Write the export into a zip file:

```powershell
svn-fucker.exe -zip ./test/psso ./test.zip
```

Add a zip password:

```powershell
svn-fucker.exe -zip --password "svn12345" ./test/psso ./test.zip
```

Zip files are not password-protected unless `--password` is provided. If `-zip` is used, the destination must be a file path, not an existing directory.

The tool reads the repository directly. Directory exports write files to the destination directory. Zip exports write exported files into a temporary directory, create the zip, and remove the temporary directory before exiting.

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
