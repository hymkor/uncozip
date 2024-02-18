package main

import (
	"errors"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"

	"github.com/mattn/go-tty"

	"github.com/hymkor/uncozip"
)

var (
	flagDebug  = flag.Bool("debug", false, "Enable debug output")
	flagTest   = flag.Bool("t", false, "Test CRC32")
	flagExDir  = flag.String("d", "", "the directory where to extract")
	flagStrict = flag.Bool("strict", false, "quit immediately on CRC-Error")
	flagDecode = flag.String("decode", "", "IANA-registered-name to decode filename")
)

func matchingPatterns(target string, patterns []string) bool {
	if patterns == nil || len(patterns) <= 0 {
		return true
	}
	for _, p := range patterns {
		matched, err := filepath.Match(p, target)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func askPassword(name string) ([]byte, error) {
	tty, err := tty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close()
	fmt.Fprintf(os.Stderr, "%s password: ", name)
	passwordString, err := tty.ReadPassword()
	if err != nil {
		return nil, err
	}
	return []byte(passwordString), nil
}

var errSkipEntry = errors.New("SKIP ENTRY")

func testEntry(cz *uncozip.CorruptedZip, patterns []string) (uint32, error) {
	fname := cz.Name()
	if cz.IsDir() {
		fmt.Fprintf(os.Stderr, "SKIP: %s\n", fname)
		return 0, errSkipEntry
	}
	if !matchingPatterns(fname, patterns) {
		return 0, errSkipEntry
	}
	h := crc32.NewIEEE()
	_, err := io.Copy(h, cz.Body())
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(os.Stderr, "%9d %s %s\n",
		cz.OriginalSize(),
		cz.LastModificationTime.Format("2006/01/02 15:04:05"),
		fname)
	return h.Sum32(), nil
}

func extractEntry(cz *uncozip.CorruptedZip, patterns []string) (uint32, error) {
	fname := cz.Name()
	if cz.IsDir() {
		fmt.Fprintln(os.Stderr, "   creating:", fname)
		if err := os.Mkdir(fname, 0644); err != nil && !os.IsExist(err) {
			return 0, err
		}
		return 0, nil
	}
	if !matchingPatterns(fname, patterns) {
		return 0, errSkipEntry
	}
	_fname := filepath.FromSlash(fname)
	fd, err := os.Create(_fname)
	if err != nil {
		var pathError *os.PathError
		if !errors.As(err, &pathError) {
			return 0, err
		}
		dir := filepath.Dir(_fname)
		if dir == "." {
			return 0, err
		}
		_, err2 := os.Stat(dir)
		if err2 == nil || !os.IsNotExist(err2) {
			return 0, err
		}
		if err2 := os.MkdirAll(dir, 0750); err2 != nil {
			return 0, err2
		}
		fmt.Fprintf(os.Stderr, "   creating: %s/\n", dir)
		fd, err = os.Create(_fname)
		if err != nil {
			return 0, err
		}
	}
	switch cz.Method() {
	case uncozip.Deflate:
		fmt.Fprintln(os.Stderr, "  inflating:", fname)
	case uncozip.Store:
		fmt.Fprintln(os.Stderr, " extracting:", fname)
	}
	h := crc32.NewIEEE()
	_, err = io.Copy(fd, io.TeeReader(cz.Body(), h))
	err1 := fd.Close()
	if err != nil {
		return 0, err
	}
	if err1 != nil {
		return 0, err1
	}
	if err := os.Chtimes(fname, cz.LastAccessTime, cz.LastModificationTime); err != nil {
		fmt.Fprintln(os.Stderr, fname, err.Error())
	}
	return h.Sum32(), nil
}

func mainForReader(r io.Reader, patterns []string) error {
	if *flagExDir != "" {
		if err := os.Chdir(*flagExDir); err != nil {
			return err
		}
	}
	cz := uncozip.New(r)
	cz.RegisterPasswordHandler(askPassword)
	if *flagDebug {
		cz.Debug = log.Println
	}
	if *flagDecode != "" {
		e, err := ianaindex.IANA.Encoding(*flagDecode)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("-decode \"%s\" not supported in golang.org/x/text/encoding/ianaindex", *flagDecode)
		}
		decoder := e.NewDecoder()
		cz.RegisterNameDecoder(func(b []byte) (string, error) {
			result, _, err := transform.Bytes(decoder, b)
			if err != nil {
				return "", err
			}
			return string(result), nil
		})
	}

	for entry := range cz.Each {
		var err error
		var checksum uint32
		if *flagTest {
			checksum, err = testEntry(entry, patterns)
		} else {
			checksum, err = extractEntry(entry, patterns)
		}
		if err == errSkipEntry {
			continue
		}
		if err != nil {
			return err
		}
		if checksum != entry.CRC32() {
			if *flagStrict {
				return fmt.Errorf("%s: CRC32 is expected %X in header, but %X",
					entry.Name(), entry.CRC32(), checksum)
			}
			fmt.Fprintf(os.Stderr,
				"NG:   %s: CRC32 is expected %X in header, but %X\n",
				entry.Name(), entry.CRC32(), checksum)
		} else if *flagDebug {
			fmt.Fprintf(os.Stderr, "%s: CRC32: header=%X , body=%X\n",
				entry.Name(), entry.CRC32(), checksum)
		}
	}
	if err := cz.Err(); err != io.EOF {
		return err
	}
	return nil
}

func mains(args []string) error {
	if len(args) <= 0 {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintf(os.Stderr, "%s %s-%s-%s by %s\n",
				filepath.Base(os.Args[0]),
				version,
				runtime.GOOS,
				runtime.GOARCH,
				runtime.Version())
			flag.PrintDefaults()
			return nil
		} else {
			return mainForReader(os.Stdin, args)
		}
	}
	fname := args[0]
	args = args[1:]
	if fname == "-" {
		return mainForReader(os.Stdin, args)
	}
	fd, err := os.Open(fname)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if strings.EqualFold(filepath.Ext(fname), ".zip") {
			return err
		}
		fd, err = os.Open(fname + ".zip")
		if err != nil {
			return err
		}
	}
	err = mainForReader(fd, args)
	err1 := fd.Close()
	if err != nil {
		return err
	}
	return err1
}

var version string

func main() {
	flag.Parse()
	if err := mains(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
