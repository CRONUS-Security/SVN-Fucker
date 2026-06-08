package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	azip "github.com/yeka/zip"
)

type runOptions struct {
	source      string
	dest        string
	zip         bool
	password    string
	passwordSet bool
}

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}

	repoPath, err := filepath.Abs(opts.source)
	if err != nil {
		return err
	}
	outPath, err := filepath.Abs(opts.dest)
	if err != nil {
		return err
	}
	if opts.passwordSet && !opts.zip {
		return errors.New("--password requires -zip")
	}
	if opts.zip {
		if info, err := os.Stat(outPath); err == nil && info.IsDir() {
			return fmt.Errorf("zip output path must be a file, got directory: %s", outPath)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	repo, err := OpenRepo(repoPath)
	if err != nil {
		return err
	}

	if !opts.zip {
		if err := repo.ExportLatest(outPath); err != nil {
			return err
		}
		fmt.Printf("checked out r%d to %s\n", repo.LatestRevision, outPath)
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "svn-export-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := repo.ExportLatest(tmpDir); err != nil {
		return err
	}
	if err := zipDir(tmpDir, outPath, opts.password); err != nil {
		return err
	}

	fmt.Printf("exported r%d to %s\n", repo.LatestRevision, outPath)
	return nil
}

func parseArgs(args []string) (runOptions, error) {
	var opts runOptions
	if len(args) == 0 {
		return opts, errors.New("missing executable name")
	}

	var positional []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-zip":
			opts.zip = true
		case "--password":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing value for --password\nusage: %s [-zip] [--password <password>] <svn-repository-path> <dest>", filepath.Base(args[0]))
			}
			if args[i] == "" {
				return opts, errors.New("--password cannot be empty")
			}
			opts.password = args[i]
			opts.passwordSet = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown option %s\nusage: %s [-zip] [--password <password>] <svn-repository-path> <dest>", arg, filepath.Base(args[0]))
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 2 {
		return opts, fmt.Errorf("usage: %s [-zip] [--password <password>] <svn-repository-path> <dest>", filepath.Base(args[0]))
	}
	opts.source = positional[0]
	opts.dest = positional[1]
	return opts, nil
}

func zipDir(srcDir, outPath, password string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}

	tmpOut := outPath + ".tmp"
	out, err := os.Create(tmpOut)
	if err != nil {
		return err
	}

	zw := azip.NewWriter(out)
	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		var w io.Writer
		if password == "" {
			w, err = zw.Create(rel)
		} else {
			w, err = zw.Encrypt(rel, password, azip.StandardEncryption)
		}
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})

	closeZipErr := zw.Close()
	closeFileErr := out.Close()
	if walkErr != nil {
		os.Remove(tmpOut)
		return walkErr
	}
	if closeZipErr != nil {
		os.Remove(tmpOut)
		return closeZipErr
	}
	if closeFileErr != nil {
		os.Remove(tmpOut)
		return closeFileErr
	}

	_ = os.Remove(outPath)
	return os.Rename(tmpOut, outPath)
}

type Repo struct {
	Path           string
	DBPath         string
	LatestRevision int
	ShardSize      int
	repCache       map[repKey][]byte
	nodeCache      map[nodeKey]*nodeRev
}

type repKey struct {
	rev    int
	offset int64
	size   int64
}

type nodeKey struct {
	rev    int
	offset int64
}

type textRep struct {
	rev      int
	offset   int64
	size     int64
	expanded int64
}

type nodeRev struct {
	id       string
	kind     string
	text     *textRep
	props    *textRep
	copyFrom string
}

type dirEntry struct {
	kind string
	id   string
}

func OpenRepo(repoPath string) (*Repo, error) {
	dbPath := filepath.Join(repoPath, "db")
	if _, err := os.Stat(filepath.Join(dbPath, "current")); err != nil {
		return nil, fmt.Errorf("not an SVN FSFS repository: %w", err)
	}

	latest, err := readCurrent(filepath.Join(dbPath, "current"))
	if err != nil {
		return nil, err
	}
	shardSize, err := readShardSize(filepath.Join(dbPath, "format"))
	if err != nil {
		return nil, err
	}

	return &Repo{
		Path:           repoPath,
		DBPath:         dbPath,
		LatestRevision: latest,
		ShardSize:      shardSize,
		repCache:       make(map[repKey][]byte),
		nodeCache:      make(map[nodeKey]*nodeRev),
	}, nil
}

func readCurrent(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0, fmt.Errorf("empty db/current")
	}
	return strconv.Atoi(parts[0])
}

func readShardSize(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "layout" && fields[1] == "sharded" {
			return strconv.Atoi(fields[2])
		}
	}
	return 0, nil
}

func (r *Repo) ExportLatest(dst string) error {
	root, err := r.rootNode()
	if err != nil {
		return err
	}
	return r.exportNode(root, dst)
}

func (r *Repo) rootNode() (*nodeRev, error) {
	revPath := r.revPath(r.LatestRevision)
	data, err := os.ReadFile(revPath)
	if err != nil {
		return nil, err
	}
	line, err := lastNonEmptyLine(data)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return nil, fmt.Errorf("invalid revision trailer in r%d", r.LatestRevision)
	}
	offset, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, err
	}
	return r.readNode(r.LatestRevision, offset)
}

func lastNonEmptyLine(data []byte) (string, error) {
	for end := len(data); end > 0; {
		for end > 0 && (data[end-1] == '\n' || data[end-1] == '\r') {
			end--
		}
		if end == 0 {
			break
		}
		start := end
		for start > 0 && data[start-1] != '\n' && data[start-1] != '\r' {
			start--
		}
		line := strings.TrimSpace(string(data[start:end]))
		if line != "" {
			return line, nil
		}
		end = start
	}
	return "", errors.New("empty revision file")
}

func (r *Repo) revPath(rev int) string {
	if r.ShardSize > 0 {
		return filepath.Join(r.DBPath, "revs", strconv.Itoa(rev/r.ShardSize), strconv.Itoa(rev))
	}
	return filepath.Join(r.DBPath, "revs", strconv.Itoa(rev))
}

func (r *Repo) readNodeByID(id string) (*nodeRev, error) {
	rev, offset, err := parseIDLocation(id)
	if err != nil {
		return nil, err
	}
	return r.readNode(rev, offset)
}

func parseIDLocation(id string) (int, int64, error) {
	idx := strings.LastIndex(id, ".r")
	if idx < 0 {
		return 0, 0, fmt.Errorf("node id has no revision location: %s", id)
	}
	rest := id[idx+2:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return 0, 0, fmt.Errorf("node id has no offset: %s", id)
	}
	rev, err := strconv.Atoi(rest[:slash])
	if err != nil {
		return 0, 0, err
	}
	offset, err := strconv.ParseInt(rest[slash+1:], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return rev, offset, nil
}

func (r *Repo) readNode(rev int, offset int64) (*nodeRev, error) {
	key := nodeKey{rev: rev, offset: offset}
	if cached, ok := r.nodeCache[key]; ok {
		return cached, nil
	}

	data, err := os.ReadFile(r.revPath(rev))
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset >= int64(len(data)) {
		return nil, fmt.Errorf("node offset out of range: r%d/%d", rev, offset)
	}

	pos := int(offset)
	n := &nodeRev{}
	for {
		line, next, err := readLine(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		line = strings.TrimRight(line, "\r")
		if line == "" {
			break
		}

		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("invalid node header at r%d/%d: %q", rev, offset, line)
		}
		switch name {
		case "id":
			n.id = value
		case "type":
			n.kind = value
		case "text":
			rep, err := parseTextRep(value)
			if err != nil {
				return nil, err
			}
			n.text = rep
		case "props":
			rep, err := parseTextRep(value)
			if err != nil {
				return nil, err
			}
			n.props = rep
		case "copyfrom":
			n.copyFrom = value
		}
	}

	if n.kind == "" {
		return nil, fmt.Errorf("node at r%d/%d has no type", rev, offset)
	}
	r.nodeCache[key] = n
	return n, nil
}

func parseTextRep(value string) (*textRep, error) {
	fields := strings.Fields(value)
	if len(fields) < 4 {
		return nil, fmt.Errorf("invalid text representation: %q", value)
	}
	rev, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil, err
	}
	offset, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, err
	}
	expanded, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return nil, err
	}
	return &textRep{rev: rev, offset: offset, size: size, expanded: expanded}, nil
}

func readLine(data []byte, pos int) (string, int, error) {
	if pos > len(data) {
		return "", pos, io.ErrUnexpectedEOF
	}
	nl := bytes.IndexByte(data[pos:], '\n')
	if nl < 0 {
		return string(data[pos:]), len(data), nil
	}
	end := pos + nl
	return string(data[pos:end]), end + 1, nil
}

func (r *Repo) exportNode(n *nodeRev, dst string) error {
	switch n.kind {
	case "dir":
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		entries, err := r.readDirEntries(n)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", n.id, err)
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !safePathName(name) {
				return fmt.Errorf("unsafe SVN path entry: %q", name)
			}
			child, err := r.readNodeByID(entries[name].id)
			if err != nil {
				return err
			}
			if child.kind != entries[name].kind {
				return fmt.Errorf("entry kind mismatch for %q: dir says %s, node says %s", name, entries[name].kind, child.kind)
			}
			if err := r.exportNode(child, filepath.Join(dst, name)); err != nil {
				return err
			}
		}
	case "file":
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		data, err := r.readNodeText(n)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0644)
	default:
		return fmt.Errorf("unknown node type: %s", n.kind)
	}
	return nil
}

func safePathName(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!strings.ContainsAny(name, `/\`) &&
		!filepath.IsAbs(name)
}

func (r *Repo) readDirEntries(n *nodeRev) (map[string]dirEntry, error) {
	if n.text == nil {
		return map[string]dirEntry{}, nil
	}
	data, err := r.readNodeText(n)
	if err != nil {
		return nil, err
	}

	entries := make(map[string]dirEntry)
	pos := 0
	for {
		line, next, err := readLine(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		line = strings.TrimRight(line, "\r")
		if line == "END" {
			break
		}
		if !strings.HasPrefix(line, "K ") {
			return nil, fmt.Errorf("invalid directory entry key marker: %q", line)
		}
		keyLen, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "K ")))
		if err != nil {
			return nil, err
		}
		if pos+keyLen > len(data) {
			return nil, io.ErrUnexpectedEOF
		}
		name := string(data[pos : pos+keyLen])
		pos += keyLen
		if pos >= len(data) || data[pos] != '\n' {
			return nil, fmt.Errorf("invalid directory entry after key %q", name)
		}
		pos++

		line, next, err = readLine(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "V ") {
			return nil, fmt.Errorf("invalid directory entry value marker for %q: %q", name, line)
		}
		valLen, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "V ")))
		if err != nil {
			return nil, err
		}
		if pos+valLen > len(data) {
			return nil, io.ErrUnexpectedEOF
		}
		value := string(data[pos : pos+valLen])
		pos += valLen
		if pos >= len(data) || data[pos] != '\n' {
			return nil, fmt.Errorf("invalid directory entry after value %q", name)
		}
		pos++

		kind, id, ok := strings.Cut(value, " ")
		if !ok {
			return nil, fmt.Errorf("invalid directory entry value: %q", value)
		}
		entries[name] = dirEntry{kind: kind, id: id}
	}
	return entries, nil
}

func (r *Repo) readNodeText(n *nodeRev) ([]byte, error) {
	if n.text == nil {
		return nil, nil
	}
	return r.readRep(n.text.rev, n.text.offset, n.text.size)
}

func (r *Repo) readRep(rev int, offset, size int64) ([]byte, error) {
	key := repKey{rev: rev, offset: offset, size: size}
	if cached, ok := r.repCache[key]; ok {
		return cached, nil
	}

	data, err := os.ReadFile(r.revPath(rev))
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset >= int64(len(data)) {
		return nil, fmt.Errorf("representation offset out of range: r%d/%d", rev, offset)
	}
	line, pos, err := readLine(data, int(offset))
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r")

	var out []byte
	switch {
	case line == "PLAIN":
		if int64(pos)+size > int64(len(data)) {
			return nil, io.ErrUnexpectedEOF
		}
		out = append([]byte(nil), data[pos:int64(pos)+size]...)
	case line == "DELTA" || strings.HasPrefix(line, "DELTA "):
		fields := strings.Fields(line)
		var base []byte
		if len(fields) == 1 {
			base = nil
		} else if len(fields) == 4 {
			baseRev, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, err
			}
			baseOff, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				return nil, err
			}
			baseSize, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil {
				return nil, err
			}
			base, err = r.readRep(baseRev, baseOff, baseSize)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("invalid delta header: %q", line)
		}
		if int64(pos)+size > int64(len(data)) {
			return nil, io.ErrUnexpectedEOF
		}
		out, err = applySvndiff(base, data[pos:int64(pos)+size])
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported representation at r%d/%d: %q", rev, offset, line)
	}

	r.repCache[key] = out
	return out, nil
}

func applySvndiff(source, stream []byte) ([]byte, error) {
	if len(stream) < 4 || !bytes.Equal(stream[:3], []byte("SVN")) {
		return nil, errors.New("invalid svndiff signature")
	}
	version := stream[3]
	if version != 0 && version != 1 {
		return nil, fmt.Errorf("unsupported svndiff version: %d", stream[3])
	}

	pos := 4
	var target []byte
	for pos < len(stream) {
		srcOff, err := readSVNInt(stream, &pos)
		if err != nil {
			return nil, err
		}
		srcLen, err := readSVNInt(stream, &pos)
		if err != nil {
			return nil, err
		}
		tgtLen, err := readSVNInt(stream, &pos)
		if err != nil {
			return nil, err
		}
		instLen, err := readSVNInt(stream, &pos)
		if err != nil {
			return nil, err
		}
		newLen, err := readSVNInt(stream, &pos)
		if err != nil {
			return nil, err
		}
		if srcOff < 0 || srcLen < 0 || tgtLen < 0 || instLen < 0 || newLen < 0 {
			return nil, errors.New("negative svndiff length")
		}
		if pos+instLen+newLen > len(stream) {
			return nil, io.ErrUnexpectedEOF
		}

		instructions := stream[pos : pos+instLen]
		pos += instLen
		newData := stream[pos : pos+newLen]
		pos += newLen
		if version == 1 {
			var err error
			instructions, err = decodeSVNDiff1Section(instructions)
			if err != nil {
				return nil, err
			}
			newData, err = decodeSVNDiff1Section(newData)
			if err != nil {
				return nil, err
			}
		}

		if srcOff+srcLen > len(source) {
			return nil, io.ErrUnexpectedEOF
		}
		srcView := source[srcOff : srcOff+srcLen]
		windowStart := len(target)
		newPos := 0
		instPos := 0
		for instPos < len(instructions) {
			op := instructions[instPos]
			instPos++
			action := op >> 6
			length := int(op & 0x3f)
			if length == 0 {
				var err error
				length, err = readSVNInt(instructions, &instPos)
				if err != nil {
					return nil, err
				}
			}
			switch action {
			case 0:
				off, err := readSVNInt(instructions, &instPos)
				if err != nil {
					return nil, err
				}
				if off < 0 || off+length > len(srcView) {
					return nil, io.ErrUnexpectedEOF
				}
				target = append(target, srcView[off:off+length]...)
			case 1:
				off, err := readSVNInt(instructions, &instPos)
				if err != nil {
					return nil, err
				}
				if off < 0 || windowStart+off > len(target) {
					return nil, io.ErrUnexpectedEOF
				}
				for i := 0; i < length; i++ {
					if windowStart+off+i >= len(target) {
						return nil, io.ErrUnexpectedEOF
					}
					target = append(target, target[windowStart+off+i])
				}
			case 2:
				if newPos+length > len(newData) {
					return nil, io.ErrUnexpectedEOF
				}
				target = append(target, newData[newPos:newPos+length]...)
				newPos += length
			default:
				return nil, fmt.Errorf("invalid svndiff instruction: %d", action)
			}
		}
		if len(target)-windowStart != tgtLen {
			return nil, fmt.Errorf("svndiff target length mismatch: got %d, want %d", len(target)-windowStart, tgtLen)
		}
		if newPos != len(newData) {
			return nil, errors.New("unused svndiff new data")
		}
	}
	return target, nil
}

func decodeSVNDiff1Section(data []byte) ([]byte, error) {
	pos := 0
	expandedLen, err := readSVNInt(data, &pos)
	if err != nil {
		return nil, err
	}
	payload := data[pos:]
	if len(payload) == expandedLen {
		return payload, nil
	}

	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	out, readErr := io.ReadAll(zr)
	closeErr := zr.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(out) != expandedLen {
		return nil, fmt.Errorf("svndiff1 section length mismatch: got %d, want %d", len(out), expandedLen)
	}
	return out, nil
}

func readSVNInt(data []byte, pos *int) (int, error) {
	var value uint64
	for {
		if *pos >= len(data) {
			return 0, io.ErrUnexpectedEOF
		}
		b := data[*pos]
		(*pos)++
		value = (value << 7) | uint64(b&0x7f)
		if b&0x80 == 0 {
			if value > uint64(^uint(0)>>1) {
				return 0, errors.New("svndiff integer overflow")
			}
			return int(value), nil
		}
	}
}
