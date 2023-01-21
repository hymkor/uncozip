package uncozip

import (
	"strings"
	"testing"
)

func TestSeekToSignatureForLocalHeader(t *testing.T) {
	var output strings.Builder
	r := strings.NewReader("HOGEHOGEPK\x03\x04")
	err := seekToSignature(r, &output)
	if err != nil {
		t.Fatal(err.Error())
		return
	}
	if out := output.String(); out != "HOGEHOGE" {
		t.Fatalf("output: expect 'HOGEHOGE' but '%s'", out)
		return
	}
}
