package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"os"
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

func (cz *CorruptedZip) Scan() error {
	br := cz.br
	var header Header
	if err := binary.Read(br, binary.LittleEndian, &header); err != nil {
		return err
	}
	name := make([]byte, header.FilenameLength)

	if _, err := io.ReadFull(br, name[:]); err != nil {
		return err
	}
	fname := string(name)
	fmt.Printf("[%04d] %s\n", header.Method, fname)

	// skip ExtendField
	// println("ExtendField:", header.ExtendFieldSize)
	if header.ExtendFieldSize > 0 {
		if _, err := br.Discard(int(header.ExtendFieldSize)); err != nil {
			return err
		}
	}
	// skip data
	// println("Compress Data:", header.CompressedSize)
	var buffer bytes.Buffer
	var w io.Writer
	if fname[len(fname)-1] == '/' {
		if err := os.Mkdir(fname, 0750); err != nil {
			if !os.IsExist(err) {
				return err
			}
		}
		w = io.Discard
	} else {
		w = &buffer
	}
	// seek the mark
	if err := seekToSignature(br, pkSignature, w); err != nil {
		return err
	}

	if buffer.Len() > 0 {
		fd, err := os.Create(fname)
		if err != nil {
			return err
		}
		defer fd.Close()
		zr := flate.NewReader(&buffer)
		defer zr.Close()
		if _, err := io.Copy(fd, zr); err != nil && err != io.EOF {
			return err
		}
	}
	return nil
}

func mains() error {
	cz, err := New(os.Stdin)
	if err != nil {
		return err
	}
	for {
		if err := cz.Scan(); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := mains(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
