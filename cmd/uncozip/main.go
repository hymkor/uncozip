package main

import (
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/mattn/go-tty"

	"github.com/hymkor/uncozip"
)

var (
	flagDebug  = flag.Bool("debug", false, "Enable debug output")
	flagTest   = flag.Bool("t", false, "Test CRC32")
	flagExDir  = flag.String("d", "", "the directory where to extract")
	flagStrict = flag.Bool("strict", false, "quit immediately on CRC-Error")
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

func testEntry(cz *uncozip.CorruptedZip, patterns []string) (uint32, error) {
	rc := cz.Body()
	if rc == nil {
		// directory
		fmt.Fprintf(os.Stderr, "SKIP: %s\n", cz.Name())
		return 0, nil
	}
	if !matchingPatterns(cz.Name(), patterns) {
		// not specified
		_, err := io.Copy(io.Discard, rc)
		if err != nil {
			return 0, err
		}
		return 0, nil
	}
	h := crc32.NewIEEE()
	_, err := io.Copy(h, rc)
	if err != nil {
		return 0, err
	}
	hour, min, second := cz.Header.Time()
	year, month, day := cz.Header.Date()
	fmt.Fprintf(os.Stderr,
		"%9d %04d/%02d/%02d %02d:%02d:%02d %s\n",
		cz.OriginalSize(),
		year, month, day, hour, min,
		second, cz.Name())
	return h.Sum32(), nil
}

func extractEntry(cz *uncozip.CorruptedZip, patterns []string) (uint32, error) {
	fname := cz.Name()
	rc := cz.Body()
	if rc == nil {
		fmt.Fprintln(os.Stderr, "   creating:", fname)
		if err := os.Mkdir(fname, 0644); err != nil && !os.IsExist(err) {
			return 0, err
		}
		return 0, nil
	}
	if !matchingPatterns(fname, patterns) {
		_, err := io.Copy(io.Discard, rc)
		return 0, err
	}
	switch cz.Header.Method {
	case uncozip.Deflated:
		fmt.Fprintln(os.Stderr, "  inflating:", fname)
	case uncozip.NotCompressed:
		fmt.Fprintln(os.Stderr, " extracting:", fname)
	}
	fd, err := os.Create(fname)
	if err != nil {
		return 0, err
	}
	h := crc32.NewIEEE()
	_, err = io.Copy(fd, io.TeeReader(rc, h))
	err1 := fd.Close()
	if err != nil {
		return 0, err
	}
	if err1 != nil {
		return 0, err1
	}
	hour, min, second := cz.Header.Time()
	year, month, day := cz.Header.Date()
	stamp := time.Date(year, time.Month(month), day, hour, min, second, 0, time.Local)
	if err := os.Chtimes(fname, stamp, stamp); err != nil {
		fmt.Fprintln(os.Stderr, fname, err.Error())
	}
	return h.Sum32(), nil
}

func mainForReader(r io.Reader, patterns []string) error {
	cz, err := uncozip.New(r)
	if err != nil {
		return err
	}
	cz.SetPasswordGetter(askPassword)
	if *flagDebug {
		cz.Debug = func(args ...any) (int, error) {
			return fmt.Fprintln(os.Stderr, args...)
		}
	}
	for cz.Scan() {
		var err error
		var checksum uint32
		if *flagTest {
			checksum, err = testEntry(cz, patterns)
		} else {
			checksum, err = extractEntry(cz, patterns)
		}
		if err != nil {
			return err
		}
		if checksum != cz.Header.CRC32 {
			if *flagStrict {
				return fmt.Errorf("%s: CRC32 is expected %X in header, but %X",
					cz.Name(), cz.Header.CRC32, checksum)
			}
			fmt.Fprintf(os.Stderr,
				"NG:   %s: CRC32 is expected %X in header, but %X\n",
				cz.Name(), cz.Header.CRC32, checksum)
		} else if *flagDebug {
			fmt.Fprintf(os.Stderr, "%s: CRC32: header=%X , body=%X\n",
				cz.Name(), cz.Header.CRC32, checksum)
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
	if *flagExDir != "" {
		if err := os.Chdir(*flagExDir); err != nil {
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
