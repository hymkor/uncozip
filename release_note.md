v0.7.1
======
Feb 22, 2023

- Command
    - Fix: error message:  
        from `-debug \"%s\" not supported in golang.org/x/text/encoding/ianaindex`  
        to `-decode \"%s\" not supported in golang.org/x/text/encoding/ianaindex"`
    - Fix: `-C "dir"` did not work when zip-input is STDIN.
- Package
    - Add method: Close(), IsDir(), RegisterNameDecoder()
    - Body() returns bytes.NewReader([]byte{}) for directories
    - The package code skips unread area instead of caller code
    - When file is skipped, do not call deflate function
    - New() is removed error value from return ones because errors never occur there

v0.7.0
======
Feb 18, 2023

- Add new option `-decode` that specifes IANA-registered-name to decode filename when UTF8 flag is not set.

v0.6.1
======
Feb 16, 2023

- **On Linux: Fix: the error `Unsupported OS` occured when the UTF8-flag for filename was not set**
- With -debug, show the extra field:
    - ID=0x7875: Info-ZIP UNIX (newer UID/GID)
    - ID=0x4453: Windows NT security descriptor (binary ACL)

v0.6.0
======
Feb 16, 2023

- Support: Extended timestamp field
    - Reflect the last update date and time and the last access date and time in 1 second units considering the timezone.

v0.5.0
======
Feb 15, 2023

- Reduce memory consumption regardless of whether a data descriptor is used or not
- Use io.ErrUnexpectedEOF instead of uncozip.ErrTooNearEOF
- Rename constants compatible to "archive/zip"
    - Deflated to Deflate
    - NotCompressed to Store

v0.4.1
======
Feb 11, 2023

- Reduce memory consumption when a data descriptor is not used.
- Add a new option: -strict: quit immediately on CRC-Error

v0.4.0
======
Feb 10, 2023

- Support ZIP64
- Support encrypted zip

v0.3.1
======
Jan 29, 2023

- Some internal fixes as a library

v0.3.0
======
Jan.24, 2023

- Show version and usage when no arguments are given and STDIN is tty
- Treat the parameter `-` as STDIN
- Add the -d option: the directory where to extract
- The pattern of filename that zip contains can now be specified

v0.2.0
======
Jan.24, 2023

- The timestamps are now restored
- Check the CRC32 value on extracting also

v0.1.0
======
Jan.22, 2023

- Fix extracting was failed when given zipfile contained zipfile.
- Add the option -t to test the CRC32 value.
- Add the option -debug to show debug-logs to the standard error output.
- The suffix .zip of the parameter can be omitted now.
- Show the phrase "inflating" or "extract" same as unzip.

v0.0.1
======
Jan.14, 2023

- The first release, the version "I think it will probably work"
