package flex

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
)

// syntheticSampleValues is the complete vocabulary that sanitize.py emits.
// testdata/sample.xml is built by regenerating from a real report's schema with
// every value replaced by one of these constants, so a value outside this set
// means either real account data leaked in or sanitize.py's vocabulary changed.
// Keep this in sync with sanitize.py's synth().
var syntheticSampleValues = map[string]bool{
	"":                true,
	"1.5":             true,
	"SAMPLE":          true,
	"SAMPLE DATA":     true,
	"VOO":             true,
	"USD":             true,
	"STK":             true,
	"BUY":             true,
	"0000":            true,
	"X":               true,
	"20240101":        true,
	"20240101;120000": true,
}

// TestSampleHasNoPrivateData asserts that the committed schema sample contains
// only synthetic values. It is the enforced guard that no real account data
// reaches testdata/sample.xml (and therefore the repository). It needs neither
// a real report nor python, so it runs in CI on every change.
func TestSampleHasNoPrivateData(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	seen := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parsing sample.xml: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, a := range se.Attr {
			seen++
			if !syntheticSampleValues[a.Value] {
				t.Errorf("sample.xml: <%s %s=%q>: value is not in the synthetic vocabulary; "+
					"this is either leaked real data or a sanitize.py change that must be mirrored here",
					se.Name.Local, a.Name.Local, a.Value)
			}
		}
	}
	if seen == 0 {
		t.Fatal("sample.xml had no attributes; sample missing or empty")
	}
}
