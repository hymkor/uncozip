package uncozip

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"golang.org/x/text/transform"

	"github.com/nyaosorg/go-windows-mbcs"
)

const (
	NotCompressed = 0
	Deflated      = 8

	bitEncrypted          = 1 << 0
	bitDataDescriptorUsed = 1 << 3
	bitEncodedUTF8        = 1 << 11

	sigSize            = 4
	dataDescriptorSize = 4 * 3
)

type LocalFileHeader struct {
	RequiredVersion  uint16
	Bits             uint16
	Method           uint16
	ModifiedTime     uint16
	ModifiedDate     uint16
	CRC32            uint32
	CompressedSize   uint32
	UncompressedSize uint32
	FilenameLength   uint16
	ExtendFieldSize  uint16
}

const (
	bitSecond = 5
	bitMin    = 6
	bitHour   = 5

	bitDay   = 5
	bitMonth = 4
	bitYear  = 7
)

func unpackBits(source uint64, bits ...int) []int {
	result := make([]int, len(bits))
	for i, bit := range bits {
		result[i] = int(source & ((1 << bit) - 1))
		source >>= bit
	}
	return result
}

// Time returns Hour, Minute, and Second of Modificated time.
func (h *LocalFileHeader) Time() (int, int, int) {
	tm := unpackBits(uint64(h.ModifiedTime), bitSecond, bitMin, bitHour)
	return tm[2], tm[1], tm[0] * 2
}

// Date returns Year, Month, and Day of Modified date.
func (h *LocalFileHeader) Date() (int, int, int) {
	dt := unpackBits(uint64(h.ModifiedDate), bitDay, bitMonth, bitYear)
	return 1980 + dt[2], dt[1], dt[0]
}

type _DataDescriptor struct {
	CRC32            uint32
	CompressedSize   uint32
	UncompressedSize uint32
}

var (
	sigLocalFileHeader             = []byte{'P', 'K', 3, 4}
	sigCentralDirectoryHeader      = []byte{'P', 'K', 1, 2}
	sigEndOfCentralDirectoryRecord = []byte{'P', 'K', 5, 6} // not used.
	sigDataDescriptor              = []byte{'P', 'K', 7, 8}
)

var (
	ErrTooNearEOF                       = errors.New("Too near EOF")
	ErrLocalFileHeaderSignatureNotFound = errors.New("Signature not found")
)

func checkDataDescriptor(buffer []byte) *_DataDescriptor {
	var desc _DataDescriptor
	start := len(buffer) - sigSize - dataDescriptorSize
	if start < 0 {
		return nil
	}
	reader := bytes.NewReader(buffer[start:])
	if err := binary.Read(reader, binary.LittleEndian, &desc); err != nil {
		return nil
	}
	return &desc
}

func (cz *CorruptedZip) seekToSignature(w io.Writer) (bool, *_DataDescriptor, error) {
	const (
		max = 100
		min = sigSize + dataDescriptorSize + sigSize
	)

	buffer := make([]byte, 0, max)
	count := 0
	for {
		ch, err := cz.br.ReadByte()
		if err != nil {
			w.Write(buffer)
			return false, nil, err
		}
		buffer = append(buffer, ch)
		count++

		switch ch {
		case sigLocalFileHeader[sigSize-1]:
			if bytes.HasSuffix(buffer, sigLocalFileHeader) {
				dd := checkDataDescriptor(buffer)
				if dd != nil {
					size := int(dd.CompressedSize)
					if size == count-sigSize-dataDescriptorSize {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize])
						cz.Debug("Found DetaDescripture without signature")
						return true, dd, nil
					}
					if size == count-sigSize-dataDescriptorSize-sigSize &&
						bytes.HasSuffix(buffer[:len(buffer)-sigSize-dataDescriptorSize], sigDataDescriptor) {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize-sigSize])
						cz.Debug("Found DataDescriptor with signature")
						return true, dd, nil
					}
				}
			}
		case sigCentralDirectoryHeader[sigSize-1]:
			if bytes.HasSuffix(buffer, sigCentralDirectoryHeader) {
				dd := checkDataDescriptor(buffer)
				if dd != nil {
					size := int(dd.CompressedSize)
					if size == count-sigSize-dataDescriptorSize {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize])
						cz.Debug("Found DetaDescripture without signature")
						return false, dd, nil
					}
					if size == count-sigSize-dataDescriptorSize-sigSize &&
						bytes.HasSuffix(buffer[:len(buffer)-sigSize-dataDescriptorSize], sigDataDescriptor) {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize-sigSize])
						cz.Debug("Found DetaDescripture with signature")
						return false, dd, nil
					}
				}
			}
		}
		if len(buffer) >= max {
			w.Write(buffer[:len(buffer)-min])
			copy(buffer[:min], buffer[len(buffer)-min:])
			buffer = buffer[:min]
		}
	}
}

type _PasswordHolder struct {
	getter   func(name string) ([]byte, error)
	lastword []byte
}

func (p *_PasswordHolder) Ready() bool {
	return p.getter != nil
}

func (p *_PasswordHolder) Ask(name string, retry bool) ([]byte, error) {
	if retry || p.lastword == nil {
		value, err := p.getter(name)
		if err != nil {
			return nil, err
		}
		p.lastword = value
	}
	return p.lastword, nil
}

type CorruptedZip struct {
	br                       *bufio.Reader
	name                     string
	body                     io.ReadCloser
	err                      error
	nextSignatureAlreadyRead bool

	originalSize64   uint64
	compressedSize64 uint64

	Debug          func(...any) (int, error)
	Header         LocalFileHeader
	passwordHolder _PasswordHolder
}

func (cz *CorruptedZip) OriginalSize() uint64 {
	if cz.originalSize64 > uint64(cz.Header.UncompressedSize) {
		return cz.originalSize64
	}
	return uint64(cz.Header.UncompressedSize)
}

func (cz *CorruptedZip) CompressedSize() uint64 {
	if cz.compressedSize64 > uint64(cz.Header.CompressedSize) {
		return cz.compressedSize64
	}
	return uint64(cz.Header.CompressedSize)
}

func (cz *CorruptedZip) SetPasswordGetter(f func(name string) ([]byte, error)) {
	cz.passwordHolder.getter = f
}

// Name returns the name of the most recent file by a call to Scan.
func (cz *CorruptedZip) Name() string {
	return cz.name
}

// Err returns the error encountered by the CorruptedZip.
func (cz *CorruptedZip) Err() error {
	return cz.err
}

// Body returns the reader of the most recent file by a call to Scan.
func (cz *CorruptedZip) Body() io.Reader {
	return cz.body
}

// New returns a CorruptedZip instance that reads a ZIP archive.
func New(r io.Reader) (*CorruptedZip, error) {
	return &CorruptedZip{
		br:    bufio.NewReader(r),
		Debug: func(...any) (int, error) { return 0, nil },
	}, nil
}

// Scan advances the CorruptedZip to the next single file in a ZIP archive.
func (cz *CorruptedZip) Scan() bool {
	if cz.body != nil {
		cz.body.Close()
		cz.body = nil
	}
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
		if bytes.Equal(signature[:], sigCentralDirectoryHeader) {
			cz.err = io.EOF
			return false
		}
		if !bytes.Equal(signature[:], sigLocalFileHeader) {
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
	cz.Debug("LocalFileHeader Name:", fname)

	fname = strings.TrimLeft(fname, "/")
	cz.name = fname

	// skip ExtendField
	cz.Debug("LocalFileHeader.ExtendFieldSize:", cz.Header.ExtendFieldSize)
	if cz.Header.ExtendFieldSize > 0 {
		var header struct {
			ID   uint16
			Size uint16
		}

		left := cz.Header.ExtendFieldSize
		for left >= 4 {
			err := binary.Read(br, binary.LittleEndian, &header)
			if err != nil {
				cz.err = fmt.Errorf("ExtendField Error: %w", err)
				return false
			}
			cz.Debug("ExtendField: ID:", header.ID, "Size:", header.Size)
			left -= 4 + header.Size
			if header.ID == 0x0001 && header.Size >= 8 {
				leftSize := header.Size
				err = binary.Read(br, binary.LittleEndian, &cz.originalSize64)
				if err != nil {
					cz.err = fmt.Errorf("ZIP64 Header: originalSize field broken: %w", err)
					return false
				}
				cz.Debug("ExtendField: ZIP64.OriginalSize:", cz.originalSize64)
				leftSize -= 8
				if leftSize >= 8 {
					err = binary.Read(br, binary.LittleEndian, &cz.compressedSize64)
					if err != nil {
						cz.err = fmt.Errorf("ZIP64 Header: compressSize field broken: %w", err)
						return false
					}
					leftSize -= 8
					cz.Debug("ExtendField: ZIP64.CompressSize:", cz.compressedSize64)
				}
				if leftSize > 0 {
					if _, err = br.Discard(int(leftSize)); err != nil {
						cz.err = fmt.Errorf("ZIP64 Header: left field broen: %w", err)
						return false
					}
				}
			} else {
				if header.Size > 0 {
					if _, err = br.Discard(int(header.Size)); err != nil {
						cz.err = err
						return false
					}
				}
			}
		}
		if left > 0 {
			if _, err := br.Discard(int(left)); err != nil {
				if err == io.EOF {
					cz.err = ErrTooNearEOF
				} else {
					cz.err = err
				}
				return false
			}
		}
	}
	cz.Debug("LocalFileHeader.CompressSize:", cz.Header.CompressedSize)
	cz.Debug("LocalFileHeader.UncompressedSize:", cz.Header.UncompressedSize)
	isDir := len(fname) > 0 && fname[len(fname)-1] == '/'
	if isDir {
		size := cz.CompressedSize()
		if size > math.MaxInt {
			panic("directory: " + fname + ":compress size is larget than math.MaxInt")
		}
		if _, err := br.Discard(int(size)); err != nil {
			cz.err = err
			return false
		}
		return true
	}

	var b io.Reader

	if (cz.Header.Bits & bitDataDescriptorUsed) != 0 {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is set")
		var buffer bytes.Buffer
		b = &buffer

		hasNextEntry, dataDescriptor, err := cz.seekToSignature(&buffer)
		if err != nil {
			if err == io.EOF {
				cz.err = ErrTooNearEOF
			} else {
				cz.err = err
			}
			return false
		}
		cz.Header.CRC32 = dataDescriptor.CRC32
		cz.Header.CompressedSize = dataDescriptor.CompressedSize
		cz.Header.UncompressedSize = dataDescriptor.UncompressedSize
		if !hasNextEntry {
			cz.br = nil
		}
		cz.nextSignatureAlreadyRead = true
	} else {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is not set")
		b = &io.LimitedReader{R: br, N: int64(cz.CompressedSize())}
		cz.nextSignatureAlreadyRead = false
	}
	if (cz.Header.Bits & bitEncrypted) != 0 {
		if !cz.passwordHolder.Ready() {
			cz.err = &ErrPassword{name: fname}
			return false
		}
		// Use cz.Header.ModifiedTime instead of CRC32.
		// The reason is unknown.
		b = transform.NewReader(b, newDecrypter(fname, &cz.passwordHolder, cz.Header.ModifiedTime))
	}
	switch cz.Header.Method {
	case Deflated:
		cz.body = flate.NewReader(b)
		return true
	case NotCompressed:
		cz.body = io.NopCloser(b)
		return true
	default:
		cz.err = fmt.Errorf("Compression Method(%d) is not supported", cz.Header.Method)
		return false
	}
}
