[![GoDev](https://pkg.go.dev/badge/github.com/hymkor/uncozip)](https://pkg.go.dev/github.com/hymkor/uncozip)

uncozip
=======

This is a command and package to UNzip COrrupted ZIP file that does not have the central directory records.

Even when the archive is so large that `zip -FF Corrupted.zip --out New.zip` fails, sometimes uncozip succeeds.  
( For example, the case Corrupted.zip is larger than 4GB )

The uncozip is also useful on non-Windows OSes to unpack archives with non-UTF8 filenames such as Shift_JIS.
(`uncozip -decode Shift_JIS foo.zip`)

Usage
-----

```
uncozip {OPTIONS} ZIPFILENAME [list...]

uncozip {OPTIONS} - [list...] < ZIPFILENAME

uncozip {OPTIONS} < ZIPFILENAME
```

* `-d string` the directory where to extract
* `-debug` Enable debug output
* `-strict` quit immediately on CRC-Error
* `-t` Test CRC32 only
* `-decode IANA-NAME` specify [IANA-registered-name][iana] to decode filename when UTF8 flag is not set (for example: `-decode Shift_JIS`)

[iana]: https://www.iana.org/assignments/character-sets/character-sets.xhtml

Install
-------

Download the binary package from [Releases](https://github.com/hymkor/uncozip/releases) and extract the executable.

### for scoop-installer

```
scoop install https://raw.githubusercontent.com/hymkor/uncozip/master/uncozip.json
```

or

```
scoop bucket add hymkor https://github.com/hymkor/scoop-bucket
scoop install uncozip
```

package "github.com/hymkor/uncozip"
===================================

Unlike "[archive/zip]", uncozip can

- read an archive from io.Reader.  
  ( "[archive/zip]" requires archive's filename[^zip.OpenReader] or io.ReaderAt and size information[^zip.NewReader] )
- handle encrypted archive.  
  ( You have to call [RegisterPasswordHandler] )
- decode filename from any encoding  
  ( You have to call [RegisterNameDecoder] )

[archive/zip]: https://pkg.go.dev/archive/zip
[RegisterPasswordHandler]: https://pkg.go.dev/github.com/hymkor/uncozip#CorruptedZip.RegisterPasswordHandler
[RegisterNameDecoder]: https://pkg.go.dev/github.com/hymkor/uncozip#CorruptedZip.RegisterNameDecoder
[^zip.OpenReader]: See also https://pkg.go.dev/archive/zip#OpenReader
[^zip.NewReader]: See also https://pkg.go.dev/archive/zip#NewReader

[Reference](https://pkg.go.dev/github.com/hymkor/uncozip)
