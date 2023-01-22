package uncozip

import (
	"strings"
	"testing"
)

func TestSeekToSignatureForLocalHeader(t *testing.T) {
	var output strings.Builder
	r := strings.NewReader("HOGEHOGEPK\x03\x04")
	cont, err := seekToSignature(r, &output)
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
	var output strings.Builder
	r := strings.NewReader("HOGEHOGEPK\x01\x02")
	cont, err := seekToSignature(r, &output)
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
