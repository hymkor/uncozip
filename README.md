uncozip
=======

This is the tool to UNzip COrrupted ZIP file that does not have the central directory records.

Even when the archive is so large that `zip -FF Corrupted.zip --out New.zip` fails, sometimes uncozip succeeds.

( Currently, it is the version "I think it will probably work" )

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
