uncozip
=======

<!-- findrun glua badges.lua | -->
[![License](https://img.shields.io/badge/License-MIT-red)](https://github.com/hymkor/uncozip/blob/master/LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/hymkor/uncozip.svg)](https://pkg.go.dev/github.com/hymkor/uncozip)
<!-- -->

This is a command and package to **UN**zip **CO**rrupted ZIP files that do not have central directory records.

Even when an archive is so large that `zip -FF Corrupted.zip --out New.zip` fails, *uncozip* sometimes succeeds.
(For example, when *Corrupted.zip* is larger than 4 GB.)

*uncozip* is also useful on non-Windows systems for unpacking archives that contain filenames encoded in non-UTF8 encodings such as Shift_JIS.
(Example: `uncozip -decode Shift_JIS foo.zip`)

Usage
-----

```
uncozip {OPTIONS} ZIPFILENAME [list...]

uncozip {OPTIONS} - [list...] < ZIPFILENAME

uncozip {OPTIONS} < ZIPFILENAME
```

* `-d string` Directory to extract into
* `-debug` Enable debug output
* `-strict` Quit immediately on CRC error
* `-t` Test CRC32 only
* `-decode IANA-NAME` Specify an [IANA-registered name][iana] used to decode filenames when the UTF-8 flag is not set
  (for example: `-decode Shift_JIS`)

[iana]: https://www.iana.org/assignments/character-sets/character-sets.xhtml

Install
-------

Download the binary package from the [Releases](https://github.com/hymkor/uncozip/releases) page and extract the executable.

### For scoop

```
scoop install https://raw.githubusercontent.com/hymkor/uncozip/master/uncozip.json
```

or

```
scoop bucket add hymkor https://github.com/hymkor/scoop-bucket
scoop install uncozip
```

package "github.com/hymkor/uncozip"
-----------------------------------

Unlike the standard `archive/zip` package, *uncozip* can:

* read an archive from an `io.Reader`
  (`archive/zip` requires the archive's filename[^zip.OpenReader] or an `io.ReaderAt` plus the size[^zip.NewReader])
* handle encrypted archives
  (you need to call [`RegisterPasswordHandler`])
* decode filenames using any encoding
  (you need to call [`RegisterNameDecoder`])

[archive/zip]: https://pkg.go.dev/archive/zip
[RegisterPasswordHandler]: https://pkg.go.dev/github.com/hymkor/uncozip#CorruptedZip.RegisterPasswordHandler
[RegisterNameDecoder]: https://pkg.go.dev/github.com/hymkor/uncozip#CorruptedZip.RegisterNameDecoder

[^zip.OpenReader]: See also https://pkg.go.dev/archive/zip#OpenReader
[^zip.NewReader]: See also https://pkg.go.dev/archive/zip#NewReader
