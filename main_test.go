package uncozip

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func noDebug(...any) (int, error) {
	return 0, nil
}

func TestSeekToSignatureForLocalHeader(t *testing.T) {

	var source bytes.Buffer
	io.WriteString(&source, "HOGEHOGE")
	dd := &_DataDescriptor{CompressedSize: 8}
	binary.Write(&source, binary.LittleEndian, dd)
	io.WriteString(&source, "PK\x03\x04")

	var output strings.Builder
	cont, _, err := seekToSignature(&source, &output, noDebug)
	if err != nil {
		t.Fatal(err.Error())
		return
	}
	if !cont {
		t.Fatal("expect local-header,but central-header found")
		return
	}
	if out := output.String(); out != "HOGEHOGE" {
		t.Fatalf("output: expect 'HOGEHOGE' but '%s'", out)
		return
	}
}

func TestSeekToSignatureForCentralDirectoryHeader(t *testing.T) {
	var source bytes.Buffer
	io.WriteString(&source, "HOGEHOGE")
	dd := &_DataDescriptor{CompressedSize: 8}
	binary.Write(&source, binary.LittleEndian, dd)
	io.WriteString(&source, "PK\x01\x02")

	var output strings.Builder
	cont, _, err := seekToSignature(&source, &output, noDebug)
	if err != nil {
		t.Fatal(err.Error())
		return
	}
	if cont {
		t.Fatal("expect central-header,but local-header found")
		return
	}
	if out := output.String(); out != "HOGEHOGE" {
		t.Fatalf("output: expect 'HOGEHOGE' but '%s'", out)
		return
	}
}
