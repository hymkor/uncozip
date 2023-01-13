package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nyaosorg/go-windows-mbcs"
)

const (
	methodNotCompressed = 0
	methodDeflated      = 8
)

type Header struct {
	RequiredVersion  int16
	Bits             int16
	Method           int16
	ModifiedTime     int16
	ModifiedDate     int16
	CRC32            int32
	CompressedSize   uint32
	UncompressedSize uint32
	FilenameLength   uint16
	ExtendFieldSize  uint16
}

var pkSignature = []byte{'P', 'K', 0x03, 0x04}

func seekToSignature(r *bufio.Reader, sig []byte, w io.Writer) error {
	L := len(sig)
	for i := 0; i < L; i++ {
		ch, err := r.ReadByte()
		if err != nil {
			if i > 0 {
				w.Write(sig[:i])
			}
			return err
		}
		if ch != sig[i] {
			if i == 0 {
				w.Write([]byte{ch})
				i = -1
			} else if ch == sig[0] {
				w.Write(sig[:i])
				i = 0
			} else {
				w.Write(sig[:i])
				w.Write([]byte{ch})
				i = -1
			}
		}
	}
	return nil
}

type CorruptedZip struct {
	br *bufio.Reader
}

func New(r io.Reader) (*CorruptedZip, error) {
	br := bufio.NewReader(r)
	if _, err := br.Discard(len(pkSignature)); err != nil {
		return nil, err
	}
	return &CorruptedZip{br: br}, nil
}

func (cz *CorruptedZip) Scan() (string, io.ReadCloser, error) {
	br := cz.br
	var header Header
	if err := binary.Read(br, binary.LittleEndian, &header); err != nil {
		return "", nil, err
	}
	name := make([]byte, header.FilenameLength)

	if _, err := io.ReadFull(br, name[:]); err != nil {
		return "", nil, err
	}
	var fname string
	if (header.Bits & (1 << 11)) == 0 {
		// not UTF8
		var err error
		fname, err = mbcs.AtoU(name, mbcs.ACP)
		if err != nil {
			return "", nil, err
		}
	} else {
		// UTF8
		fname = string(name)
	}
	fname = strings.TrimLeft(fname, "/")

	// skip ExtendField
	// println("ExtendField:", header.ExtendFieldSize)
	if header.ExtendFieldSize > 0 {
		if _, err := br.Discard(int(header.ExtendFieldSize)); err != nil {
			return "", nil, err
		}
	}
	// skip data
	// println("Compress Data:", header.CompressedSize)
	var buffer bytes.Buffer
	var w io.Writer

	isDir := fname[len(fname)-1] == '/'
	if isDir {
		w = io.Discard
	} else {
		w = &buffer
	}

	// seek the mark
	if err := seekToSignature(br, pkSignature, w); err != nil && err != io.EOF {
		return "", nil, err
	}
	if isDir {
		return fname, nil, nil
	}
	switch header.Method {
	case methodDeflated:
		zr := flate.NewReader(&buffer)
		return fname, zr, nil
	case methodNotCompressed:
		return fname, io.NopCloser(&buffer), nil
	default:
		return fname, nil, fmt.Errorf("Compression Method(%d) is not supported",
			header.Method)
	}
}

func main1(r io.Reader) error {
	cz, err := New(r)
	if err != nil {
		return err
	}
	for {
		fname, rc, err := cz.Scan()
		if err != nil {
			return err
		}
		if rc != nil {
			fmt.Fprintln(os.Stderr, "Extract", fname)
			fd, err := os.Create(fname)
			if err != nil {
				rc.Close()
				return err
			}
			io.Copy(fd, rc)
			rc.Close()
			fd.Close()
		} else {
			fmt.Fprintln(os.Stderr, "Mkdir", fname)
			if err := os.Mkdir(fname, 0644); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
}

func mains(args []string) error {
	if len(args) <= 0 {
		return main1(os.Stdin)
	}
	for _, fname := range args {
		fd, err := os.Open(fname)
		if err != nil {
			return err
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
	if err := mains(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
