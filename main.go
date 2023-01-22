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

	bitDataDescriptorUsed = 1 << 3
	bitEncodedUTF8        = 1 << 11
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

var (
	ErrTooNearEOF                       = errors.New("Too near EOF")
	ErrLocalFileHeaderSignatureNotFound = errors.New("Signature not found")
)

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
	br                       *bufio.Reader
	name                     string
	body                     io.ReadCloser
	err                      error
	nextSignatureAlreadyRead bool
}

func (cz *CorruptedZip) Name() string {
	return cz.name
}

func (cz *CorruptedZip) Err() error {
	return cz.err
}

func (cz *CorruptedZip) Body() io.ReadCloser {
	return cz.body
}

func New(r io.Reader) (*CorruptedZip, error) {
	return &CorruptedZip{br: bufio.NewReader(r)}, nil
}

func (cz *CorruptedZip) Scan() bool {
	br := cz.br
	if br == nil {
		cz.err = io.EOF
		return false
	}
	cz.err = nil

	if !cz.nextSignatureAlreadyRead {
		var signature [4]byte
		if _, err := io.ReadFull(br, signature[:]); err != nil {
			cz.err = ErrTooNearEOF
			return false
		}
		if bytes.Equal(signature[:], _CentralDirectoryHeader) {
			cz.err = io.EOF
			return false
		}
		if !bytes.Equal(signature[:], _LocalFileHeaderSignature) {
			cz.err = ErrLocalFileHeaderSignatureNotFound
			return false
		}
	}

	var header Header
	if err := binary.Read(br, binary.LittleEndian, &header); err != nil {
		cz.err = err
		return false
	}
	name := make([]byte, header.FilenameLength)

	if _, err := io.ReadFull(br, name[:]); err != nil {
		cz.err = err
		return false
	}
	var fname string
	if (header.Bits & bitEncodedUTF8) != 0 {
		fname = string(name)
	} else {
		var err error
		fname, err = mbcs.AtoU(name, mbcs.ACP)
		if err != nil {
			cz.err = err
			return false
		}
	}
	fname = strings.TrimLeft(fname, "/")
	cz.name = fname

	// skip ExtendField
	// println("ExtendField:", header.ExtendFieldSize)
	if header.ExtendFieldSize > 0 {
		if _, err := br.Discard(int(header.ExtendFieldSize)); err != nil {
			cz.err = err
			return false
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

	if (header.Bits & bitDataDescriptorUsed) != 0 {
		//println("bitDataDescriptorUsed is not set")
		if err := seekToSignature(br, w); err != nil {
			if err == io.EOF {
				cz.br = nil
			} else {
				cz.err = err
				return false
			}
		}
		cz.nextSignatureAlreadyRead = true
	} else {
		// println("bitDataDescriptorUsed is not set")
		if _, err := io.CopyN(w, br, int64(header.CompressedSize)); err != nil {
			cz.err = err
			return false
		}
		cz.nextSignatureAlreadyRead = false
	}
	if isDir {
		cz.body = nil
		return true
	}
	switch header.Method {
	case methodDeflated:
		cz.body = flate.NewReader(&buffer)
		return true
	case methodNotCompressed:
		cz.body = io.NopCloser(&buffer)
		return true
	default:
		cz.body = nil
		cz.err = fmt.Errorf("Compression Method(%d) is not supported", header.Method)
		return false
	}
}
