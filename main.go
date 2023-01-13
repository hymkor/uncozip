package uncozip

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
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

var _LocalFileHeaderSignature = []byte{'P', 'K', 3, 4}
var _CentralDirectoryHeader = []byte{'P', 'K', 1, 2}

func seekToSignature(r *bufio.Reader, w io.Writer) error {
	for {
		// Test the first byte is 'P'
		ch, err := r.ReadByte()
		if err != nil {
			return err
		}
		if ch != 'P' {
			w.Write([]byte{ch})
			continue
		}

		// Test the second byte is 'K'
		ch, err = r.ReadByte()
		if err != nil {
			w.Write(_LocalFileHeaderSignature[:1])
			return err
		}
		if ch != 'K' {
			w.Write(_LocalFileHeaderSignature[:1])
			if ch == 'P' {
				r.UnreadByte()
			} else {
				w.Write([]byte{ch})
			}
			continue
		}

		// Test the third byte is '\x03' or '\0x01'
		ch, err = r.ReadByte()
		if err != nil {
			w.Write(_LocalFileHeaderSignature[:2])
			return err
		}
		switch ch {
		default:
			w.Write(_LocalFileHeaderSignature[:2])
			w.Write([]byte{ch})
			continue
		case 'P':
			r.UnreadByte()
			w.Write(_LocalFileHeaderSignature[:2])
			continue
		case '\x03': // next header
			ch, err = r.ReadByte()
			if err != nil {
				w.Write(_LocalFileHeaderSignature[:3])
				return err
			}
			if ch == 'P' {
				r.UnreadByte()
				w.Write(_LocalFileHeaderSignature[:3])
				continue
			}
			if ch != '\x04' {
				w.Write(_LocalFileHeaderSignature[:3])
				w.Write([]byte{ch})
				continue
			}
			return nil
		case '\x01': // central directory header
			ch, err = r.ReadByte()
			if err != nil {
				w.Write(_CentralDirectoryHeader[:3])
				return err
			}
			if ch == 'P' {
				r.UnreadByte()
				w.Write(_CentralDirectoryHeader[:3])
				continue
			}
			if ch != '\x02' {
				w.Write(_CentralDirectoryHeader[:3])
				w.Write([]byte{ch})
				continue
			}
			return io.EOF
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
