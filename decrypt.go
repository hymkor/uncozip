package uncozip

import (
	"errors"
	"hash/crc32"

	"golang.org/x/text/transform"
)

// https://pkware.cachefly.net/webdocs/APPNOTE/APPNOTE-6.3.9.TXT
// 6.1.5

// var _ transform.Transformer = &decrypter{}

type decrypter struct {
	name      string // for error message
	check     uint16
	pwdHolder *PasswordHolder
	key       [3]uint32
	first     bool
}

func newDecrypter(name string, pwdHolder *PasswordHolder, check uint16) *decrypter {
	this := &decrypter{name: name, check: check, pwdHolder: pwdHolder}
	this.Reset()
	return this
}

func (d *decrypter) Reset() {
	d.first = true
	d.key[0] = 305419896
	d.key[1] = 591751049
	d.key[2] = 878082192
}

func (d *decrypter) updateKeys(n byte) {
	d.key[0] = _crc32(d.key[0], uint32(n))
	d.key[1] = d.key[1] + (d.key[0] & 0xFF)
	d.key[1] = d.key[1]*134775813 + 1
	d.key[2] = _crc32(d.key[2], (d.key[1]>>24)&0xFF)
}

func (d *decrypter) decryptByte() byte {
	tmp := d.key[2] | 2
	return byte((tmp * (tmp ^ 1)) >> 8)
}

func (d *decrypter) decrypt(b byte) byte {
	tmp := b ^ d.decryptByte()
	d.updateKeys(tmp)
	return tmp
}

var PasswordError = errors.New("password error")

func (d *decrypter) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	if d.first {
		const CHECKSIZE = 12
		if len(src) < CHECKSIZE {
			return nDst, nSrc, transform.ErrShortSrc
		}
		for i := 0; ; i++ {
			if i >= 3 {
				return 0, 0, PasswordError
			}
			pwd, err := d.pwdHolder.Ask(d.name, i > 0)
			if err != nil {
				return 0, 0, err
			}
			for _, b := range pwd {
				d.updateKeys(b)
			}
			var check [CHECKSIZE]byte
			for j := 0; j < len(check); j++ {
				check[j] = d.decrypt(src[j])
			}
			if check[CHECKSIZE-1] == byte(d.check>>8) {
				break
			}
			nSrc = 0
			d.Reset()
		}
		nSrc = CHECKSIZE
		src = src[CHECKSIZE:]
		d.first = false
	}
	for _, b := range src {
		if nDst >= len(dst) {
			return nDst, nSrc, transform.ErrShortDst
		}
		dst[nDst] = d.decrypt(b)
		nSrc++
		nDst++
	}
	return nDst, nSrc, nil
}

func _crc32(n1 uint32, n2 uint32) uint32 {
	return crc32.IEEETable[(n1^(n2&0xFF))&0xFF] ^ (n1 >> 8)
}
