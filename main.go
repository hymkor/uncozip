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
	NotCompressed = 0
	Deflated      = 8

	bitDataDescriptorUsed = 1 << 3
	bitEncodedUTF8        = 1 << 11

	sigSize = 4
)

type LocalFileHeader struct {
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

func seekToSignature(r io.ByteReader, w io.Writer) (bool, error) {
	const max = 100

	buffer := make([]byte, 0, max)
	for {
		ch, err := r.ReadByte()
		if err != nil {
			return false, err
		}
		buffer = append(buffer, ch)

		switch ch {
		case _LocalFileHeaderSignature[sigSize-1]:
			if bytes.HasSuffix(buffer, _LocalFileHeaderSignature) {
				w.Write(buffer[:len(buffer)-sigSize])
				return true, nil
			}
		case _CentralDirectoryHeader[sigSize-1]:
			if bytes.HasSuffix(buffer, _CentralDirectoryHeader) {
				w.Write(buffer[:len(buffer)-sigSize])
				return false, nil
			}
		}
		if len(buffer) >= max {
			w.Write(buffer[:len(buffer)-sigSize])
			copy(buffer[:sigSize], buffer[len(buffer)-sigSize:])
			buffer = buffer[:sigSize]
		}
	}
}

type CorruptedZip struct {
	br                       *bufio.Reader
	name                     string
	body                     io.ReadCloser
	err                      error
	nextSignatureAlreadyRead bool

	Debug  func(...any) (int, error)
	Header LocalFileHeader
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
	return &CorruptedZip{
		br:    bufio.NewReader(r),
		Debug: func(...any) (int, error) { return 0, nil },
	}, nil
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
			if err == io.EOF {
				cz.err = ErrTooNearEOF
			} else {
				cz.err = err
			}
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

	if err := binary.Read(br, binary.LittleEndian, &cz.Header); err != nil {
		if err == io.EOF {
			cz.err = ErrTooNearEOF
		} else {
			cz.err = err
		}
		return false
	}
	name := make([]byte, cz.Header.FilenameLength)

	if _, err := io.ReadFull(br, name[:]); err != nil {
		if err == io.EOF {
			cz.err = ErrTooNearEOF
		} else {
			cz.err = err
		}
		return false
	}
	var fname string
	if (cz.Header.Bits & bitEncodedUTF8) != 0 {
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
	cz.Debug("LocalFileHeader.ExtendField:", cz.Header.ExtendFieldSize)
	if cz.Header.ExtendFieldSize > 0 {
		if _, err := br.Discard(int(cz.Header.ExtendFieldSize)); err != nil {
			if err == io.EOF {
				cz.err = ErrTooNearEOF
			} else {
				cz.err = err
			}
			return false
		}
	}
	cz.Debug("LocalFileHeader.Compress Data:", cz.Header.CompressedSize)
	var buffer bytes.Buffer
	var w io.Writer

	isDir := len(fname) > 0 && fname[len(fname)-1] == '/'
	if isDir {
		w = io.Discard
	} else {
		w = &buffer
	}

	if (cz.Header.Bits & bitDataDescriptorUsed) != 0 {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is set")
		cont, err := seekToSignature(br, w)
		if err != nil {
			if err == io.EOF {
				cz.err = ErrTooNearEOF
			} else {
				cz.err = err
			}
			return false
		}
		if !cont {
			cz.br = nil
		}
		cz.nextSignatureAlreadyRead = true
	} else {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is not set")
		if _, err := io.CopyN(w, br, int64(cz.Header.CompressedSize)); err != nil {
			if err == io.EOF {
				cz.err = ErrTooNearEOF
			} else {
				cz.err = err
			}
			return false
		}
		cz.nextSignatureAlreadyRead = false
	}
	if isDir {
		cz.body = nil
		return true
	}
	switch cz.Header.Method {
	case Deflated:
		cz.body = flate.NewReader(&buffer)
		return true
	case NotCompressed:
		cz.body = io.NopCloser(&buffer)
		return true
	default:
		cz.body = nil
		cz.err = fmt.Errorf("Compression Method(%d) is not supported", cz.Header.Method)
		return false
	}
}
