package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hymkor/uncozip"
)

var flagDebug = flag.Bool("debug", false, "Enable debug output")

func main1(r io.Reader) error {
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
			_, err = io.Copy(fd, rc)
			rc.Close()
			fd.Close()
			if err != nil {
				return err
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
	if len(args) <= 0 {
		return main1(os.Stdin)
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
		err = main1(fd)
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
