package uncozip

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

var (
	_LocalFileHeaderSignature    = []byte{'P', 'K', 3, 4}
	_CentralDirectoryHeader      = []byte{'P', 'K', 1, 2}
	_EndOfCentralDirectoryRecord = []byte{'P', 'K', 5, 6} // not used.
	_DataDescriptor              = []byte{'P', 'K', 7, 8} // not used.
)

var ErrTooNearEOF = errors.New("Too near EOF")

func seekToSignature(r io.ByteReader, w io.Writer) error {
	const max = 100

	buffer := make([]byte, 0, max)
	for {
		// Test the first byte is 'P'
		ch, err := r.ReadByte()
		if err != nil {
			if err == io.EOF {
				return ErrTooNearEOF
			}
			return err
		}
		buffer = append(buffer, ch)

		switch ch {
		case _LocalFileHeaderSignature[3]:
			if bytes.HasSuffix(buffer, _LocalFileHeaderSignature) {
				w.Write(buffer[:len(buffer)-4])
				return nil
			}
		case _CentralDirectoryHeader[3]:
			if bytes.HasSuffix(buffer, _CentralDirectoryHeader) {
				w.Write(buffer[:len(buffer)-4])
				return io.EOF
			}
		}
		if len(buffer) >= max {
			w.Write(buffer[:len(buffer)-4])
			copy(buffer[:4], buffer[len(buffer)-4:])
			buffer = buffer[:4]
		}
	}
}

type CorruptedZip struct {
	br *bufio.Reader
}

func New(r io.Reader) (*CorruptedZip, error) {
	br := bufio.NewReader(r)
	if _, err := br.Discard(len(_LocalFileHeaderSignature)); err != nil {
		return nil, err
	}
	return &CorruptedZip{br: br}, nil
}

func (cz *CorruptedZip) Scan() (string, io.ReadCloser, error) {
	br := cz.br
	if br == nil {
		return "", nil, io.EOF
	}
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

	isDir := len(fname) > 0 && fname[len(fname)-1] == '/'
	if isDir {
		w = io.Discard
	} else {
		w = &buffer
	}

	// seek the mark
	if err := seekToSignature(br, w); err != nil {
		if err == io.EOF {
			cz.br = nil
		} else {
			return "", nil, err
		}
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
