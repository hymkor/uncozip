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
	Store   = 0
	Deflate = 8

	bitEncrypted          = 1 << 0
	bitDataDescriptorUsed = 1 << 3
	bitEncodedUTF8        = 1 << 11

	sigSize            = 4
	dataDescriptorSize = 4 * 3
)

var decompressors = map[uint16]func(io.Reader) io.ReadCloser{
	Store:   io.NopCloser,
	Deflate: flate.NewReader,
}

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

// time returns Hour, Minute, and Second of Modificated time.
func (h *_LocalFileHeader) time() (int, int, int) {
	tm := unpackBits(uint64(h.ModifiedTime), bitSecond, bitMin, bitHour)
	return tm[2], tm[1], tm[0] * 2
}

// date returns Year, Month, and Day of Modified date.
func (h *_LocalFileHeader) date() (int, int, int) {
	dt := unpackBits(uint64(h.ModifiedDate), bitDay, bitMonth, bitYear)
	return 1980 + dt[2], dt[1], dt[0]
}

// stamp returns the modificated time of the most recent file by a call to Scan.
func (h *_LocalFileHeader) stamp() time.Time {
	hour, min, second := h.time()
	year, month, day := h.date()
	return time.Date(year, time.Month(month), day, hour, min, second, 0, time.Local)
}

type _DataDescriptor struct {
	CRC32            uint32
	CompressedSize   uint32
	UncompressedSize uint32
}

var (
	sigLocalFileHeader        = []byte{'P', 'K', 3, 4}
	sigCentralDirectoryHeader = []byte{'P', 'K', 1, 2}
	// sigEndOfCentralDirectoryRecord = []byte{'P', 'K', 5, 6} // not used.
	sigDataDescriptor = []byte{'P', 'K', 7, 8}
)

var (
	ErrLocalFileHeaderSignatureNotFound = errors.New("signature not found")
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

func seekToSignature(r io.ByteReader, w io.Writer, debug func(...any)) (bool, *_DataDescriptor, error) {
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
	closers                  []func()
	br                       *bufio.Reader
	name                     string
	rawFileData              io.Reader
	err                      error
	nextSignatureAlreadyRead bool

	LastModificationTime time.Time
	LastAccessTime       time.Time
	CreationTime         time.Time

	originalSize   func() uint64
	compressedSize func() uint64
	crc32          func() uint32

	hasNextEntry func() bool
	bgErr        func() error
	fnameDecoder func([]byte) (string, error)

	header         _LocalFileHeader
	passwordHolder _PasswordHolder

	// Debug outputs debug-log. When the field is not set, debug-log is dropped.
	Debug func(...any)
}

// originalSize returns the current file's uncompressed size written in "local file header" or "data descriptor".
// When an "data descriptor" exists, it waits until file data stream is read all.
// This field is sed by Scan method
func (cz *CorruptedZip) OriginalSize() uint64 {
	return cz.originalSize()
}

// CompressedSize returns the current file's compressed size written in "local file header" or "data descriptor".
// When an "data descriptor" exists, it waits until file data stream is read all.
// This field is sed by Scan method
func (cz *CorruptedZip) CompressedSize() uint64 {
	return cz.compressedSize()
}

// CRC32 returns the current file's CRC32 written in "local file header" or "data descriptor".
// When an "data descriptor" exists, it waits until file data stream is read all.
// This field is sed by Scan method
func (cz *CorruptedZip) CRC32() uint32 {
	return cz.crc32()
}

// Method returns the mothod to compress the current entry data
func (cz *CorruptedZip) Method() uint16 {
	return cz.header.Method
}

// RegisterPasswordHandler sets a callback function to query password to an user.
func (cz *CorruptedZip) RegisterPasswordHandler(f func(filename string) (password []byte, err error)) {
	cz.passwordHolder.getter = f
}

// RegisterNameDecoder sets callback function to translate nonUTF8 filename to UTF8
func (cz *CorruptedZip) RegisterNameDecoder(f func([]byte) (string, error)) {
	cz.fnameDecoder = f
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
	if cz.rawFileData == nil {
		return bytes.NewReader([]byte{})
	}
	f, ok := decompressors[cz.header.Method]
	if !ok {
		return bytes.NewReader([]byte{})
	}
	r := f(cz.rawFileData)
	cz.closers = append(cz.closers, func() { r.Close() })
	return r
}

// IsDir returns true when the current file is a directory.
func (cz *CorruptedZip) IsDir() bool {
	return cz.rawFileData == nil
}

func defaultFNameDecoder(b []byte) (string, error) {
	name, err := mbcs.AnsiToUtf8(b, mbcs.ACP)
	if err == mbcs.ErrUnsupportedOs {
		err = nil
	}
	return string(name), err
}

// New returns a CorruptedZip instance that reads a ZIP archive.
func New(r io.Reader) *CorruptedZip {
	return &CorruptedZip{
		br:           bufio.NewReader(r),
		Debug:        func(...any) {},
		bgErr:        func() error { return nil },
		hasNextEntry: func() bool { return true },
		closers:      make([]func(), 0, 2),
		fnameDecoder: defaultFNameDecoder,
	}
}

func readFilenameField(r io.Reader, n uint16, utf8 bool, decoder func([]byte) (string, error)) (string, error) {
	name := make([]byte, n)

	if _, err := io.ReadFull(r, name[:]); err != nil {
		return "", err
	}
	var fname string
	if utf8 {
		fname = string(name)
	} else {
		var err error
		fname, err = decoder(name)
		if err != nil {
			return fname, err
		}
	}
	return strings.TrimLeft(fname, "/"), nil
}

func readZIP64(r io.Reader, cz *CorruptedZip) error {
	var origSize uint64
	err := binary.Read(r, binary.LittleEndian, &origSize)
	if err != nil {
		return fmt.Errorf("ZIP64 Header: originalSize field broken: %w", err)
	}
	cz.originalSize = func() uint64 { return origSize }

	cz.Debug("  ExtendField: ZIP64.OriginalSize:", origSize)
	var compSize uint64
	err = binary.Read(r, binary.LittleEndian, &compSize)
	if err != nil {
		return fmt.Errorf("ZIP64 Header: compressSize field broken: %w", err)
	}
	cz.compressedSize = func() uint64 { return compSize }
	cz.Debug("  ExtendField: ZIP64.CompressSize:", compSize)
	return nil
}

func readAsSecondsSince1970(r io.Reader) (tm time.Time, err error) {
	var secondsSince1970 uint32
	err = binary.Read(r, binary.LittleEndian, &secondsSince1970)
	if err != nil {
		return
	}
	tm = time.Unix(int64(secondsSince1970), 0)
	return
}

func readTimeStamp(r io.Reader, cz *CorruptedZip) error {
	var bitflag [1]byte
	err := binary.Read(r, binary.LittleEndian, &bitflag)
	if err != nil {
		return fmt.Errorf("extended fileStamp bit field can not read: %w", err)
	}
	if (bitflag[0] & 1) != 0 {
		cz.LastModificationTime, err = readAsSecondsSince1970(r)
		if err != nil {
			return fmt.Errorf("last modified dateTime: %w", err)
		}
		cz.Debug("  Last Modification Time:", cz.LastModificationTime)
	}
	if (bitflag[0] & 2) != 0 {
		cz.LastAccessTime, err = readAsSecondsSince1970(r)
		if err != nil {
			return fmt.Errorf("last access dateTime: %w", err)
		}
		cz.Debug("  Last Access Time:", cz.LastAccessTime)
	}
	if (bitflag[0] & 4) != 0 {
		cz.CreationTime, err = readAsSecondsSince1970(r)
		if err != nil {
			return fmt.Errorf("creation time: %w", err)
		}
		cz.Debug("  Create Time:", cz.CreationTime)
	}
	return nil
}

func readWinACL(r io.Reader, cz *CorruptedZip) error {
	cz.Debug("  Ignore: Windows NT security descriptor (binary ACL)")
	var data struct {
		BSize   uint16
		Version byte
		CType   uint16
		EACRC   uint32
	}
	if err := binary.Read(r, binary.LittleEndian, &data); err != nil {
		return err
	}
	cz.Debug("  Version:", data.BSize)
	cz.Debug("  CType:", data.CType)
	cz.Debug("  EACRC:", data.EACRC)
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, r); err == nil {
		cz.Debug("  compressed SD data:", buffer.Bytes())
	}
	return nil
}

func readNewUnixExtraField(r io.Reader, cz *CorruptedZip) error {
	cz.Debug("  Ignore: New Unix Extra Field")
	var versionAndUidSize [2]byte
	if _, err := io.ReadFull(r, versionAndUidSize[:]); err != nil {
		return err
	}
	cz.Debug("  Version:", versionAndUidSize[0])

	uid := make([]byte, versionAndUidSize[1])
	if _, err := io.ReadFull(r, uid[:]); err != nil {
		return err
	}
	cz.Debug("  UID:", uid)

	var gidSize [1]byte
	if _, err := io.ReadFull(r, gidSize[:]); err != nil {
		return err
	}

	gid := make([]byte, gidSize[0])
	if _, err := io.ReadFull(r, gid[:]); err != nil {
		return err
	}
	cz.Debug("  GID:", uid)
	return nil
}

const (
	idZIP64   = 0x0001
	idWinACL  = 0x4453
	idStamp   = 0x5455
	idNewUnix = 0x7875
)

var extendFieldFunc = map[uint16]func(r io.Reader, cz *CorruptedZip) error{
	idZIP64:   readZIP64,
	idStamp:   readTimeStamp,
	idWinACL:  readWinACL,
	idNewUnix: readNewUnixExtraField,
}

func readExtendField(r io.Reader, n uint16, cz *CorruptedZip) (err error) {
	cz.Debug("ExtendFieldSize:", n)
	if n <= 0 {
		return
	}
	lr := &io.LimitedReader{R: r, N: int64(n)}
	for lr.N >= 4 {
		var header struct {
			ID   uint16
			Size uint16
		}
		if e := binary.Read(lr, binary.LittleEndian, &header); e != nil {
			return fmt.Errorf("ExtendField Error: %w", e)
		}
		cz.Debug("ExtendField: ID:", header.ID, "Size:", header.Size)

		llr := &io.LimitedReader{R: lr, N: int64(header.Size)}
		if f, ok := extendFieldFunc[header.ID]; ok {
			if err := f(llr, cz); err != nil {
				return err
			}
		} else {
			cz.Debug("  Unknown extended field")
		}
		if llr.N > 0 {
			io.Copy(io.Discard, llr)
		}
	}
	if lr.N > 0 {
		io.Copy(io.Discard, lr)
	}
	return nil
}

// Close releases resources used on the Scan method previous called.
func (cz *CorruptedZip) Close() {
	for i := len(cz.closers) - 1; i >= 0; i-- {
		cz.closers[i]()
	}
	cz.closers = cz.closers[:0]
}

func (cz *CorruptedZip) scan() (err error) {
	cz.Close()
	if err := cz.bgErr(); err != nil {
		return err
	}
	if !cz.hasNextEntry() {
		return io.EOF
	}
	cz.rawFileData = nil

	if !cz.nextSignatureAlreadyRead {
		var signature [4]byte
		if _, err := io.ReadFull(cz.br, signature[:]); err != nil {
			return err
		}
		if bytes.Equal(signature[:], sigCentralDirectoryHeader) {
			return io.EOF
		}
		if !bytes.Equal(signature[:], sigLocalFileHeader) {
			return ErrLocalFileHeaderSignatureNotFound
		}
	}

	if err := binary.Read(cz.br, binary.LittleEndian, &cz.header); err != nil {
		return err
	}
	cz.originalSize = func() uint64 { return uint64(cz.header.UncompressedSize) }
	cz.compressedSize = func() uint64 { return uint64(cz.header.CompressedSize) }
	cz.crc32 = func() uint32 { return cz.header.CRC32 }
	cz.bgErr = func() error { return nil }
	cz.hasNextEntry = func() bool { return true }
	cz.CreationTime = cz.header.stamp()
	cz.LastModificationTime = cz.header.stamp()
	cz.LastAccessTime = cz.header.stamp()
	cz.Debug("LocalFileHeader")
	cz.Debug("  ModificatedDate/Time(DOS) :", cz.LastModificationTime)
	cz.Debug("  CompressSize:", cz.header.CompressedSize)
	cz.Debug("  UncompressedSize:", cz.header.UncompressedSize)

	cz.name, err = readFilenameField(cz.br, cz.header.FilenameLength, (cz.header.Bits&bitEncodedUTF8) != 0, cz.fnameDecoder)
	if err != nil {
		return err
	}
	cz.Debug("  Name:", cz.name)

	if err := readExtendField(cz.br, cz.header.ExtendFieldSize, cz); err != nil {
		return err
	}

	isDir := len(cz.name) > 0 && cz.name[len(cz.name)-1] == '/'
	if isDir {
		if (cz.header.Bits & bitDataDescriptorUsed) != 0 {
			hasNextEntry, _, err := seekToSignature(cz.br, io.Discard, cz.Debug)
			if err != nil {
				return err
			}
			cz.hasNextEntry = func() bool { return hasNextEntry }
			cz.nextSignatureAlreadyRead = true
		} else {
			size := cz.CompressedSize()
			if size > math.MaxInt {
				panic("directory: " + cz.name + ":compress size is larget than math.MaxInt")
			}
			if _, err := cz.br.Discard(int(size)); err != nil {
				return err
			}
			cz.nextSignatureAlreadyRead = false
		}
		return nil
	}

	if (cz.header.Bits & bitDataDescriptorUsed) != 0 {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is set")

		c := make(chan readResult)

		ch := &lazyReadResult{channel: c}
		cz.originalSize = func() uint64 {
			return uint64(ch.Value().UncompressedSize)
		}
		cz.compressedSize = func() uint64 {
			return uint64(ch.Value().CompressedSize)
		}
		cz.crc32 = func() uint32 {
			return ch.Value().CRC32
		}
		cz.bgErr = func() error {
			return ch.Value().err
		}
		cz.hasNextEntry = func() bool {
			return ch.Value().hasNextEntry
		}

		pipeR, pipeW := io.Pipe()
		cz.closers = append(cz.closers, func() { pipeR.Close() })
		cz.nextSignatureAlreadyRead = true
		cz.rawFileData = pipeR

		go func() {
			hasNextEntry, dataDescriptor, err := seekToSignature(cz.br, pipeW, cz.Debug)
			pipeW.Close()
			c <- readResult{
				_DataDescriptor: dataDescriptor,
				hasNextEntry:    hasNextEntry,
				err:             err,
			}
		}()
	} else {
		cz.Debug("LocalFileHeader.Bits: bitDataDescriptorUsed is not set")
		cz.rawFileData = &io.LimitedReader{R: cz.br, N: int64(cz.CompressedSize())}
		cz.nextSignatureAlreadyRead = false
	}
	if (cz.header.Bits & bitEncrypted) != 0 {
		if !cz.passwordHolder.Ready() {
			return &ErrPassword{name: cz.name}
		}
		// Use cz.header.ModifiedTime instead of CRC32.
		// The reason is unknown.
		cz.rawFileData = transform.NewReader(cz.rawFileData, newDecrypter(cz.name, &cz.passwordHolder, cz.header.ModifiedTime))
	}
	cz.closers = append(cz.closers, func() { io.Copy(io.Discard, cz.rawFileData) })
	if _, ok := decompressors[cz.header.Method]; !ok {
		return fmt.Errorf("compression method(%d) is not supported", cz.header.Method)
	}
	return nil
}

// Scan advances the CorruptedZip to the next single file in a ZIP archive.
func (cz *CorruptedZip) Scan() bool {
	err := cz.scan()
	if err == io.EOF {
		cz.err = nil // means no error
	} else {
		cz.err = err
	}
	return err == nil
}

// Each is the function for rangefunc.
func (cz *CorruptedZip) Each(f func(*CorruptedZip) bool) {
	for {
		err := cz.scan()
		if err != nil {
			if err != io.EOF {
				cz.err = err
			}
			break
		}
		if !f(cz) {
			break
		}
	}
}
