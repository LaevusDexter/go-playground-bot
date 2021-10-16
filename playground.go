package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/tools/go/ast/astutil"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var bufferPool = &sync.Pool{}

func PostToPlayground(src string) error {
	req, err := http.NewRequest("POST", "https://play.golang.org/share", bytes.NewBufferString(src))
	if err != nil {
		return fmt.Errorf("PostToPlayground: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("User-Agent", "Go_Playground")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PostToPlayground: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("PostToPlayground: got non-200 response: %s", resp.Status)
	}

	linkID, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("PostToPlayground: %v", err)
	}

	return fmt.Errorf(`https://play.golang.org/p/%s`,  b2s(linkID))
}

type Event struct {
	Message string
	Kind    string        // "stdout" or "stderr"
	Delay   time.Duration // time to wait before printing Message
}

type playgroundResponse struct {
	Errors      string
	Events      []Event
	Status      int
	IsTest      bool
	TestsFailed int

	// VetErrors, if non-empty, contains any vet errors. It is
	// only populated if request.WithVet was true.
	VetErrors string `json:",omitempty"`
	// VetOK reports whether vet ran & passsed. It is only
	// populated if request.WithVet was true. Only one of
	// VetErrors or VetOK can be non-zero.
	VetOK bool `json:",omitempty"`
}

func CompileAndRun(str string, debug bool) ([]byte, error) {
	code := findCodeBlock(str)
	if code == "" {
		return nil, fmt.Errorf("Why, give me the code, human! Ye, right after the go command, go and write it down right there, okay? I don't mind if you use a code block. \n" +
			"Here's a list of options available:\n" +
			"-debug, or -d\n" +
			"-plain or -p\n" +
			"What do they do? Hmm, my boss can't word it correctly neither am I. So 'try it and see' is your way to go.\n" +
			"Good luck!")
	}

	closeOnce := false
	importMap := make(map[string]bool)
	importIgnoreMap := make(map[string]bool)

	var nextImports []func(fset *token.FileSet, f *ast.File)

	retries := 0

	buf, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		buf = bytes.NewBuffer(make([]byte, 0, len(code)))
	}
	defer buf.Reset()
	defer bufferPool.Put(buf)

	buf.WriteString(code)

	var debugMemory string
	var lazyLines []string
	retryCounter := 0
	fset := token.NewFileSet()
retry:
	retryCounter++
	f, err := parser.ParseFile(fset, "", buf, 0)
	if cannotFix := tryToFixErrors(err, &buf, &lazyLines, f, fset); cannotFix != nil {
		if debug {
			return nil, fmt.Errorf("%s\n------\n%v", b2s(buf.Bytes()), cannotFix)
		}

		return nil, cannotFix
	} else if err != nil {
		if retryCounter > 100 {
			return nil, fmt.Errorf("SOMETHING WENT WRONG, TOO MANY FIX RETRIES: %v", err)
		}

		goto retry
	}

	if !hasFunc(f, "main") {
		var lazyCode string

		if lazyLines != nil {
			lazyCode = strings.Join(lazyLines, "\n")
			lazyCode = fmt.Sprintf(lazyTemplate, lazyCode)
		} else {
			lazyCode = "\nfunc main() {}\n"
		}

		if len(lazyCode)+buf.Len() > buf.Cap() {
			buf = bytes.NewBuffer(append(buf.Bytes(), s2b(lazyCode)...))
		} else {
			buf.WriteString(lazyCode)
		}

		goto retry
	}

	for _, addImport := range nextImports {
		addImport(fset, f)
	}

	if hasImport(f, "time") {
		if !hasImport(f, "unsafe") {
			astutil.AddNamedImport(fset, f, "_", "unsafe")
		}
	}

	buf.Reset()

	err = format.Node(buf, fset, f)
	if err != nil {
		return nil, fmt.Errorf("CompileAndRun: %v", err)
	}

	if hasImport(f, "time") {
		buf.WriteString(randomTimeTemplate)
	}

	data := struct {
		Body    string
		WithVet bool
	}{b2s(buf.Bytes()), false}

	if debug {
		debugMemory = string(buf.Bytes()) + "\n------\n"
	}

	b, err := json.Marshal(&data)
	if err != nil {
		return nil, fmt.Errorf("CompileAndRun: %v", err)
	}

	if cap(b) > buf.Cap() {
		buf = bytes.NewBuffer(b)
	} else {
		buf.Reset()

		buf.Write(b)
	}

	resp, err := http.Post("https://play.golang.org/compile", "application/json", buf)
	if err != nil {
		return nil, fmt.Errorf("CompileAndRun: %v", err)
	}

	if !closeOnce {
		defer resp.Body.Close()

		closeOnce = true
	}

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("CompileAndRun: %v", err)
	}

	if b[0] != '{' {
		nextImports = parseImportError(b, importMap, importIgnoreMap)

		if len(nextImports) == 0 || retries >= 1 {
			if debug {
				b = append([]byte(debugMemory), b...)
			}

			// error within b
			goto ret
		}

		buf.Reset()
		buf.WriteString(code)
		lazyLines = lazyLines[:0]

		retries++
		goto retry
	}

	if !strings.HasPrefix(b2s(b), `{"Errors":""`) {
		nextImports = parseImportError(b, importMap, importIgnoreMap)

		if len(nextImports) != 0 && retries < 1 {
			buf.Reset()
			buf.WriteString(code)
			lazyLines = lazyLines[:0]

			retries++
			goto retry
		}
	}

	if debug {
		var res playgroundResponse

		err = json.Unmarshal(b, &res)
		if err != nil {
			log.Println(err)

			return b, nil
		}

		res.Errors = debugMemory + res.Errors

		bt, err := json.Marshal(res)
		if err != nil {
			log.Println(err)

			return b, nil
		}

		b = bt
	}

ret:
	return b, nil
}

func tryToFixErrors(err error, buf **bytes.Buffer, lazyLines *[]string, f *ast.File, fset *token.FileSet) error {
	if err == nil {
		return nil
	}

	if strings.Contains(err.Error(), "expected 'package'") {
		if len(packageStub)+(*buf).Len() > (*buf).Cap() {
			*buf = bytes.NewBuffer(append([]byte(packageStub), (*buf).Bytes()...))
		} else {
			body := (*buf).Bytes()

			body = body[:len(body)+len(packageStub)]
			copy(body[len(packageStub):], body)
			copy(body, packageStub)

			*buf = bytes.NewBuffer(body)
		}

		return nil
	} else if strings.Contains(err.Error(), "expected declaration") {

		body := (*buf).Bytes()
		for _, d := range f.Decls {
			fn, ok := d.(*ast.BadDecl)
			if !ok {
				continue
			}

			fl := fset.File(fn.From)
			pos := fset.Position(fn.From)
			start := pos.Offset
			if pos.Line  == fl.LineCount() {
				*lazyLines = append(*lazyLines, string(body[start:]))

				body = body[:start]
				*buf = bytes.NewBuffer(body)

				break
			}

			end := fset.Position(fl.LineStart(pos.Line + 1)).Offset
			*lazyLines = append(*lazyLines, string(body[start:end - len("\n")]))

			n := copy(body[start:], body[end:])
			body = body[:start + n]
			*buf = bytes.NewBuffer(body)

			break
		}

		return nil
	}

	return err
}

func parseImportError(str []byte, imports map[string]bool, ignore map[string]bool) (rimp []func(fset *token.FileSet, f *ast.File)) {

	var (
		i, start, offset int
		char, yes        bool
		result           []byte
		res, tmp         string
		once             = make(map[string]bool)
	)

next:
	if start >= len(str) {
		return
	}

	offset = bytes.Index(str[start:], prog)
	if offset == -1 {
		return
	}

	start += offset

	offset = bytes.Index(str[start:], undefined)
	if offset == -1 {
		return
	}

	start += offset

	for i = start + len(undefined); i < len(str); i++ {
		switch str[i] {
		case ' ', '\t', '\n', '\r', '\\', '"':
			if char {
				res = string(result)
				result = result[:0]

				char = false
				start = i + 1

				goto try_ignore_catch
			}
		default:
			result = append(result, str[i])
			char = true
		}
	}

	if len(result) == 0 {
		return
	}

	res = string(result)
	start = i + 1

try_ignore_catch:
	if _, yes = once[res]; yes {
		goto next
	}

	if _, yes = ignore[res]; yes {
		if _, yes = imports[res]; yes {
			delete(imports, res)

			/*for _, imp := range stdImports {
				if len(imp) >= len(res) && imp[len(imp)-len(res):] == res {
					rimp = append(rimp, func(fset *token.FileSet, f *ast.File) {
						astutil.DeleteImport(fset, f, imp)
					})

					goto next
				}
			}*/
		}

		goto next
	}

	ignore[res] = true
	once[res] = true

	for _, imp := range stdImports {
		if len(imp) < len(res) {
			continue
		}

		tmp = imp[len(imp)-len(res):]
		if len(tmp) != len(imp) && imp[len(imp)-len(res)-1] != '/' {
			continue
		}

		if tmp == res {

			rimp = append(rimp, func(fset *token.FileSet, f *ast.File) {
				astutil.AddImport(fset, f, imp)
			})

			imports[res] = true

			goto next
		}
	}

	goto next
}

func findCodeBlock(str string) string {
	buf := make([]byte, 0, 16)
	isSingleLine := true
	btPos :=  make([]int, 0, 2)

	for i := 0; i < len(str); i++ {
		buf = append(buf , str[i])

		switch b2s(buf) {
		case "```golang":
			btPos[0] += len("lang")
			buf = buf[:0]
		case "```golan":
		case "```gola":
		case "```gol":
		case "```go":
			btPos[0] += len("go")
		case "```g":
		case "```":
			if len(btPos) == 0 {
				btPos = append(btPos, i + len("`"))

				continue
			}

			btPos = append(btPos, i - len("``"))
		case "``":
			if isSingleLine {
				isSingleLine = false
				btPos = btPos[:0]
			}
		case "`":
			if isSingleLine {
				btPos = append(btPos, i + len("`"))

				if len(btPos) == 2 {
					return str[btPos[0]: btPos[1] - len("`")]
				}
			}
		default:
			buf = buf[:0]
		}
	}

	if len(btPos) == 1 {
		return str[btPos[0]:]
	}

	if len(btPos) == 0 {
		return str
	}

	return str[btPos[0]: btPos[len(btPos) - 1]]
}

func hasFunc(f *ast.File, name string) bool {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name == name {
			return true
		}
	}

	return false
}

func hasImport(f *ast.File, path string) bool {
	is := importSpec(f, path)
	if is != nil {
		return true
	}

	return false
}

func importSpec(f *ast.File, path string) *ast.ImportSpec {
	for _, s := range f.Imports {
		if importPath(s) == path {
			return s
		}
	}

	return nil
}

func importPath(s *ast.ImportSpec) string {
	t, err := strconv.Unquote(s.Path.Value)
	if err != nil {
		return ""
	}

	return t
}

var undefined = []byte("undefined:")
var prog = []byte("./prog.go")
const packageStub = "package main\n"

var stdImports = []string{
	"archive",
	"archive/tar",
	"archive/zip",
	"bufio",
	"builtin",
	"bytes",
	"compress",
	"compress/bzip2",
	"compress/flate",
	"compress/gzip",
	"compress/lzw",
	"compress/zlib",
	"container",
	"container/heap",
	"container/list",
	"container/ring",
	"context",
	"crypto",
	"crypto/aes",
	"crypto/cipher",
	"crypto/des",
	"crypto/dsa",
	"crypto/ecdsa",
	"crypto/ed25519",
	"crypto/elliptic",
	"crypto/hmac",
	"crypto/md5",
	"crypto/rc4",
	"crypto/rsa",
	"crypto/sha1",
	"crypto/sha256",
	"crypto/sha512",
	"crypto/subtle",
	"crypto/tls",
	"crypto/x509",
	"crypto/x509/pkix",
	"database",
	"database/sql",
	"database/sql/driver",
	"debug",
	"debug/dwarf",
	"debug/elf",
	"debug/gosym",
	"debug/macho",
	"debug/pe",
	"debug/plan9obj",
	"encoding",
	"encoding/ascii85",
	"encoding/asn1",
	"encoding/base32",
	"encoding/base64",
	"encoding/binary",
	"encoding/csv",
	"encoding/gob",
	"encoding/hex",
	"encoding/json",
	"encoding/pem",
	"encoding/xml",
	"errors",
	"expvar",
	"flag",
	"fmt",
	"go",
	"go/ast",
	"go/build",
	"go/constant",
	"go/doc",
	"go/format",
	"go/importer",
	"go/parser",
	"go/printer",
	"go/scanner",
	"go/token",
	"go/types",
	"hash",
	"hash/adler32",
	"hash/crc32",
	"hash/crc64",
	"hash/fnv",
	"hash/maphash",
	"html",
	"html/template",
	"image",
	"image/color",
	"image/color/palette",
	"image/draw",
	"image/gif",
	"image/jpeg",
	"image/png",
	"index",
	"index/suffixarray",
	"io",
	"io/ioutil",
	"log",
	"log/syslog",
	"math",
	"math/big",
	"math/bits",
	"math/cmplx",
	"math/rand",
	"mime",
	"mime/multipart",
	"mime/quotedprintable",
	"net",
	"net/http",
	"net/http/cgi",
	"net/http/cookiejar",
	"net/http/fcgi",
	"net/http/httptest",
	"net/http/httptrace",
	"net/http/httputil",
	"net/http/pprof",
	"net/mail",
	"net/rpc",
	"net/rpc/jsonrpc",
	"net/smtp",
	"net/textproto",
	"net/url",
	"os",
	"os/exec",
	"os/signal",
	"os/user",
	"path",
	"path/filepath",
	"plugin",
	"reflect",
	"regexp",
	"regexp/syntax",
	"runtime",
	"runtime/cgo",
	"runtime/debug",
	"runtime/msan",
	"runtime/pprof",
	"runtime/race",
	"runtime/trace",
	"sort",
	"strconv",
	"strings",
	"sync",
	"sync/atomic",
	"syscall",
	"syscall/js",
	"testing",
	"testing/iotest",
	"testing/quick",
	"text",
	"text/scanner",
	"text/tabwriter",
	"text/template",
	"text/template/parseCommand",
	"time",
	"unicode",
	"unicode/utf16",
	"unicode/utf8",
	"unsafe",
}

var lazyTemplate = `
type суперсекретнаяразработкакгб interface{}
func 这他妈跟我们说好的不一样啊() interface{} {
	%s

	return суперсекретнаяразработкакгб(0)
}

func main() {
	result := 这他妈跟我们说好的不一样啊()
	if result != суперсекретнаяразработкакгб(0) { fmt.Println(result) }
}
`

var randomTimeTemplate = `
//go:linkname ⴰⵣⵓⵍ runtime.fastrandn
//go:nosplit
func ⴰⵣⵓⵍ(n uint32) uint32

//go:linkname ⵖⵓⵔⴽ time.Now
func ⵖⵓⵔⴽ() time.Time {
	return time.Unix(int64(ⴰⵣⵓⵍ(2147483647)), 0)
}
`