package main

import (
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hymkor/uncozip"
)

var (
	flagDebug = flag.Bool("debug", false, "Enable debug output")
	flagTest  = flag.Bool("t", false, "Test CRC32")
)

func testCRC32FromReader(r io.Reader) error {
	cz, err := uncozip.New(r)
	if err != nil {
		return err
	}
	if *flagDebug {
		cz.Debug = func(args ...any) (int, error) {
			return fmt.Fprintln(os.Stderr, args...)
		}
	}
	for cz.Scan() {
		rc := cz.Body()
		if rc != nil {
			h := crc32.NewIEEE()
			_, err = io.Copy(h, rc)
			err1 := rc.Close()
			if err != nil {
				return err
			}
			if err1 != nil {
				return err1
			}
			checksum := h.Sum32()
			if *flagDebug {
				fmt.Fprintf(os.Stderr, "%s: CRC32: header=%X , body=%X\n",
					cz.Name(), cz.Header.CRC32, checksum)
			}
			if checksum != cz.Header.CRC32 {
				fmt.Fprintf(os.Stderr,
					"NG:   %s: CRC32 is expected %X in header, but %X\n",
					cz.Name(), cz.Header.CRC32, checksum)
			} else {
				fmt.Fprintf(os.Stderr, "OK:   %s\n", cz.Name())
			}
		} else {
			fmt.Fprintf(os.Stderr, "SKIP: %s\n", cz.Name())
		}
	}
	if err := cz.Err(); err != io.EOF {
		return err
	}
	return nil
}

func unzipFromReader(r io.Reader) error {
	cz, err := uncozip.New(r)
	if err != nil {
		return err
	}
	if *flagDebug {
		cz.Debug = func(args ...any) (int, error) {
			return fmt.Fprintln(os.Stderr, args...)
		}
	}
	for cz.Scan() {
		fname := cz.Name()
		rc := cz.Body()
		if rc != nil {
			switch cz.Header.Method {
			case uncozip.Deflated:
				fmt.Fprintln(os.Stderr, "  inflating:", fname)
			case uncozip.NotCompressed:
				fmt.Fprintln(os.Stderr, " extracting:", fname)
			}
			fd, err := os.Create(fname)
			if err != nil {
				rc.Close()
				return err
			}
			h := crc32.NewIEEE()
			_, err = io.Copy(fd, io.TeeReader(rc, h))
			err1 := rc.Close()
			err2 := fd.Close()
			if err != nil {
				return err
			}
			if err1 != nil {
				return err1
			}
			if err2 != nil {
				return err2
			}
			checksum := h.Sum32()
			if *flagDebug {
				fmt.Fprintf(os.Stderr, "%s: CRC32: header=%X , body=%X\n",
					cz.Name(), cz.Header.CRC32, checksum)
			}
			if checksum != cz.Header.CRC32 {
				fmt.Fprintf(os.Stderr,
					"NG:   %s: CRC32 is expected %X in header, but %X\n",
					cz.Name(), cz.Header.CRC32, checksum)
			}
		} else {
			fmt.Fprintln(os.Stderr, "   creating:", fname)
			if err := os.Mkdir(fname, 0644); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
	if err := cz.Err(); err != io.EOF {
		return err
	}
	return nil
}

func mains(args []string) error {
	f := unzipFromReader
	if *flagTest {
		f = testCRC32FromReader
	}
	if len(args) <= 0 {
		return f(os.Stdin)
	}
	for _, fname := range args {
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
		err = f(fd)
		fd.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	flag.Parse()
	if err := mains(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
