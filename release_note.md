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
