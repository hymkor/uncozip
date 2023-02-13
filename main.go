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
	"sync"
	"time"

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

type _LocalFileHeader struct {
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
func (h *_LocalFileHeader) Time() (int, int, int) {
	tm := unpackBits(uint64(h.ModifiedTime), bitSecond, bitMin, bitHour)
	return tm[2], tm[1], tm[0] * 2
}

// Date returns Year, Month, and Day of Modified date.
func (h *_LocalFileHeader) Date() (int, int, int) {
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

func seekToSignature(r io.ByteReader, w io.Writer, debug func(...any) (int, error)) (bool, *_DataDescriptor, error) {
	const (
		max = 100
		min = sigSize + dataDescriptorSize + sigSize
	)

	buffer := make([]byte, 0, max)
	count := 0
	for {
		ch, err := r.ReadByte()
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
						debug("Found DetaDescripture without signature")
						return true, dd, nil
					}
					if size == count-sigSize-dataDescriptorSize-sigSize &&
						bytes.HasSuffix(buffer[:len(buffer)-sigSize-dataDescriptorSize], sigDataDescriptor) {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize-sigSize])
						debug("Found DataDescriptor with signature")
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
						debug("Found DetaDescripture without signature")
						return false, dd, nil
					}
					if size == count-sigSize-dataDescriptorSize-sigSize &&
						bytes.HasSuffix(buffer[:len(buffer)-sigSize-dataDescriptorSize], sigDataDescriptor) {
						w.Write(buffer[:len(buffer)-sigSize-dataDescriptorSize-sigSize])
						debug("Found DetaDescripture with signature")
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

type readResult struct {
	*_DataDescriptor
	hasNextEntry bool
	err          error
}

type lazyReadResult struct {
	channel chan readResult
	once    sync.Once
	value   readResult
}

func (u *lazyReadResult) Value() *readResult {
	u.once.Do(func() {
		u.value = <-u.channel
		close(u.channel)
	})
	return &u.value
}

// CorruptedZip is a reader for a ZIP archive that reads from io.Reader instead of io.ReaderAt
type CorruptedZip struct {
	closers                  []io.Closer
	br                       *bufio.Reader
	name                     string
	body                     io.Reader
	err                      error
	nextSignatureAlreadyRead bool

	OriginalSize   func() uint64
	CompressedSize func() uint64
	CRC32          func() uint32
	hasNextEntry   func() bool
	bgErr          func() error

	header         _LocalFileHeader
	passwordHolder _PasswordHolder

	Debug func(...any) (int, error)
}

// Method returns the mothod to compress the current entry data
func (cz *CorruptedZip) Method() uint16 {
	return cz.header.Method
}

// SetPasswordGetter sets a callback function to query password to an user.
func (cz *CorruptedZip) SetPasswordGetter(f func(name string) ([]byte, error)) {
	cz.passwordHolder.getter = f
}

// Name returns the name of the most recent file by a call to Scan.
func (cz *CorruptedZip) Name() string {
	return cz.name
}

// Err returns the error encountered by the CorruptedZip.
func (cz *CorruptedZip) Err() error {
	if cz.err != nil {
		return cz.err
	}
	return cz.bgErr()
}

// Body returns the reader of the most recent file by a call to Scan.
func (cz *CorruptedZip) Body() io.Reader {
	return cz.body
}

// Stamp returns the modificated time of the most recent file by a call to Scan.
func (cz *CorruptedZip) Stamp() time.Time {
	hour, min, second := cz.header.Time()
	year, month, day := cz.header.Date()
	return time.Date(year, time.Month(month), day, hour, min, second, 0, time.Local)
}

// New returns a CorruptedZip instance that reads a ZIP archive.
func New(r io.Reader) (*CorruptedZip, error) {
	return &CorruptedZip{
		br:           bufio.NewReader(r),
		Debug:        func(...any) (int, error) { return 0, nil },
		bgErr:        func() error { return nil },
		hasNextEntry: func() bool { return true },
	}, nil
}

// Scan advances the CorruptedZip to the next single file in a ZIP archive.
func (cz *CorruptedZip) Scan() bool {
	if cz.closers != nil {
		for _, c := range cz.closers {
			c.Close()
		}
		cz.closers = cz.closers[:0]
	}
	if err := cz.bgErr(); err != nil {
		cz.err = err
		return false
	}
	if !cz.hasNextEntry() {
		cz.err = io.EOF
		return false
	}

	cz.err = nil
	cz.body = nil

	if !cz.nextSignatureAlreadyRead {
		var signature [4]byte
		if _, err := io.ReadFull(cz.br, signature[:]); err != nil {
			if err == io.EOF {
				cz.err = io.ErrUnexpectedEOF
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

	if err := binary.Read(cz.br, binary.LittleEndian, &cz.header); err != nil {
		if err == io.EOF {
			cz.err = io.ErrUnexpectedEOF
		} else {
			cz.err = err
		}
		return false
	}
	cz.OriginalSize = func() uint64 { return uint64(cz.header.UncompressedSize) }
	cz.CompressedSize = func() uint64 { return uint64(cz.header.CompressedSize) }
	cz.CRC32 = func() uint32 { return cz.header.CRC32 }
	cz.bgErr = func() error { return nil }
	cz.hasNextEntry = func() bool { return true }

	name := make([]byte, cz.header.FilenameLength)

	if _, err := io.ReadFull(cz.br, name[:]); err != nil {
		if err == io.EOF {
			cz.err = io.ErrUnexpectedEOF
		} else {
			cz.err = err
		}
		return false
	}
	var fname string
	if (cz.header.Bits & bitEncodedUTF8) != 0 {
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

	cz.Debug("LocalFileHeader.ExtendFieldSize:", cz.header.ExtendFieldSize)
	if cz.header.ExtendFieldSize > 0 {
		var header struct {
			ID   uint16
			Size uint16
		}

		left := cz.header.ExtendFieldSize
		for left >= 4 {
			err := binary.Read(cz.br, binary.LittleEndian, &header)
			if err != nil {
				cz.err = fmt.Errorf("ExtendField Error: %w", err)
				return false
			}
			cz.Debug("ExtendField: ID:", header.ID, "Size:", header.Size)
			left -= 4 + header.Size
			if header.ID == 0x0001 && header.Size >= 8 {
				leftSize := header.Size
				var origSize uint64
				err = binary.Read(cz.br, binary.LittleEndian, &origSize)
				if err != nil {
					cz.err = fmt.Errorf("ZIP64 Header: originalSize field broken: %w", err)
					return false
				}
				cz.OriginalSize = func() uint64 { return origSize }

				cz.Debug("ExtendField: ZIP64.OriginalSize:", origSize)
				leftSize -= 8
				if leftSize >= 8 {
					var compSize uint64
					err = binary.Read(cz.br, binary.LittleEndian, &compSize)
					if err != nil {
						cz.err = fmt.Errorf("ZIP64 Header: compressSize field broken: %w", err)
						return false
					}
					cz.CompressedSize = func() uint64 { return compSize }
					leftSize -= 8
					cz.Debug("ExtendField: ZIP64.CompressSize:", compSize)
				}
				if leftSize > 0 {
					if _, err = cz.br.Discard(int(leftSize)); err != nil {
						cz.err = fmt.Errorf("ZIP64 Header: left field broen: %w", err)
						return false
					}
				}
			} else {
				if header.Size > 0 {
					if _, err = cz.br.Discard(int(header.Size)); err != nil {
						cz.err = err
						return false
					}
				}
			}
		}
		if left > 0 {
			if _, err := cz.br.Discard(int(left)); err != nil {
				if err == io.EOF {
					cz.err = io.ErrUnexpectedEOF
				} else {
					cz.err = err
				}
				return false
			}
		}
	}
	cz.Debug("LocalFileHeader.CompressSize:", cz.header.CompressedSize)
	cz.Debug("LocalFileHeader.UncompressedSize:", cz.header.UncompressedSize)
	isDir := len(fname) > 0 && fname[len(fname)-1] == '/'
	if isDir {
		if (cz.header.Bits & bitDataDescriptorUsed) != 0 {
			hasNextEntry, _, err := seekToSignature(cz.br, io.Discard, cz.Debug)
			if err != nil {
				cz.err = err
				return false
			}
			cz.hasNextEntry = func() bool { return hasNextEntry }
			cz.nextSignatureAlreadyRead = true
		} else {
			size := cz.CompressedSize()
			if size > math.MaxInt {
				panic("directory: " + fname + ":compress size is larget than math.MaxInt")
			}
			if _, err := cz.br.Discard(int(size)); err != nil {
				cz.err = err
				return false
			}
			cz.nextSignatureAlreadyRead = false
		}
		return true
	}

	var b io.Reader

	if (cz.header.Bits & bitDataDescriptorUsed) != 0 {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is set")

		c := make(chan readResult)

		ch := &lazyReadResult{channel: c}
		cz.OriginalSize = func() uint64 {
			return uint64(ch.Value().UncompressedSize)
		}
		cz.CompressedSize = func() uint64 {
			return uint64(ch.Value().CompressedSize)
		}
		cz.CRC32 = func() uint32 {
			return ch.Value().CRC32
		}
		cz.bgErr = func() error {
			return ch.Value().err
		}
		cz.hasNextEntry = func() bool {
			return ch.Value().hasNextEntry
		}

		pipeR, pipeW := io.Pipe()
		cz.closers = append(cz.closers, pipeR)
		b = pipeR

		go func() {
			hasNextEntry, dataDescriptor, err := seekToSignature(cz.br, pipeW, cz.Debug)
			pipeW.Close()
			cz.nextSignatureAlreadyRead = true
			c <- readResult{
				_DataDescriptor: dataDescriptor,
				hasNextEntry:    hasNextEntry,
				err:             err,
			}
		}()
	} else {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is not set")
		b = &io.LimitedReader{R: cz.br, N: int64(cz.CompressedSize())}
		cz.nextSignatureAlreadyRead = false
	}
	if (cz.header.Bits & bitEncrypted) != 0 {
		if !cz.passwordHolder.Ready() {
			cz.err = &ErrPassword{name: fname}
			return false
		}
		// Use cz.header.ModifiedTime instead of CRC32.
		// The reason is unknown.
		b = transform.NewReader(b, newDecrypter(fname, &cz.passwordHolder, cz.header.ModifiedTime))
	}
	switch cz.header.Method {
	case Deflated:
		zr := flate.NewReader(b)
		cz.body = zr
		cz.closers = append(cz.closers, zr)
		return true
	case NotCompressed:
		cz.body = b
		return true
	default:
		cz.err = fmt.Errorf("Compression Method(%d) is not supported", cz.header.Method)
		return false
	}
}
