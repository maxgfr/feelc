package dmnxml_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/loader"
)

// FuzzImportDMN asserts DMN XML import never panics on arbitrary bytes (XML parse + DSL generation), and
// that any DSL it does generate itself compiles or errors without panicking — import feeds the compiler,
// so the two must compose safely on adversarial input.
func FuzzImportDMN(f *testing.F) {
	f.Add([]byte(gradeDMN))    // a valid DMN (from import_test.go)
	f.Add([]byte(priorityDMN)) // PRIORITY + <outputValues>
	f.Add([]byte("<definitions/>"))
	f.Add([]byte("<definitions><decision/></definitions>"))
	f.Add([]byte("not xml"))
	f.Add([]byte(""))
	f.Fuzz(func(_ *testing.T, data []byte) {
		rules, _, err := dmnxml.Import(data)
		if err != nil {
			return // malformed DMN: a clean error, nothing to compile
		}
		_, _, _, _ = loader.Compile([]byte(rules)) // generated DSL must not panic the front-end
	})
}
