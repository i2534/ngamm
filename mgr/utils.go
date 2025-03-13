package mgr

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	COMMON_DIR_MODE  os.FileMode = 0755
	COMMON_FILE_MODE os.FileMode = 0644
)

type CustomTime struct {
	time.Time
}

func FromTime(t time.Time) CustomTime {
	return CustomTime{Time: t}
}
func Now() CustomTime {
	return FromTime(time.Now())
}
func (t CustomTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	lt := t.In(TIME_LOC)
	return json.Marshal(lt.Format("2006-01-02 15:04:05"))
}
func (t *CustomTime) UnmarshalJSON(data []byte) error {
	if string(data) == `""` {
		t.Time = time.Time{}
		return nil
	}
	var str string
	if e := json.Unmarshal(data, &str); e != nil {
		return e
	}
	lt, e := time.ParseInLocation("2006-01-02 15:04:05", str, TIME_LOC)
	if e != nil {
		return e
	}
	t.Time = lt
	return nil
}

func Local() *time.Location {
	as, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		as = time.Local
	}
	return as
}

type ExtRoot struct {
	*os.Root
}

func OpenRoot(path string) (*ExtRoot, error) {
	if !IsExist(path) {
		if e := os.MkdirAll(path, COMMON_DIR_MODE); e != nil {
			return nil, e
		}
	}
	r, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	return &ExtRoot{Root: r}, nil
}
func (r *ExtRoot) join(path ...string) string {
	fn := "."
	if len(path) > 0 {
		fn = filepath.Join(path...)
	}
	return fn
}
func (r *ExtRoot) SafeOpenRoot(path ...string) (*ExtRoot, error) {
	p := r.join(path...)
	if !r.IsExist(p) {
		if e := r.Mkdir(p, COMMON_DIR_MODE); e != nil {
			return nil, e
		}
	}
	sub, e := r.OpenRoot(p)
	if e != nil {
		return nil, e
	}
	return &ExtRoot{sub}, nil
}
func (r *ExtRoot) ReadDir(path ...string) ([]fs.DirEntry, error) {
	f, e := r.Open(r.join(path...))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return f.ReadDir(-1)
}
func (r *ExtRoot) AbsPath(path ...string) (string, error) {
	f, e := r.Open(r.join(path...))
	if e != nil {
		return "", e
	}
	defer f.Close()
	return filepath.Abs(f.Name())
}
func (r *ExtRoot) IsExist(path ...string) bool {
	if _, e := r.Stat(r.join(path...)); os.IsNotExist(e) {
		return false
	}
	return true
}
func (r *ExtRoot) EveryLine(name string, fx func(string, int) bool) error {
	f, e := r.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		if !fx(scanner.Text(), i) {
			return nil
		}
		i++
	}
	if e := scanner.Err(); e != nil {
		return e
	}
	return nil
}
func (r *ExtRoot) OpenReader(path ...string) (*os.File, error) {
	f, e := r.Open(r.join(path...))
	if e != nil {
		return nil, e
	}
	return f, nil
}
func (r *ExtRoot) OpenWriter(name string, perm ...os.FileMode) (*os.File, error) {
	d := filepath.Dir(name)
	if !r.IsExist(d) {
		if e := r.Mkdir(d, COMMON_DIR_MODE); e != nil {
			return nil, e
		}
	}
	fm := COMMON_FILE_MODE
	if len(perm) > 0 {
		fm = perm[0]
	}
	f, e := r.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fm)
	if e != nil {
		return nil, e
	}
	return f, nil
}
func (r *ExtRoot) ReadAll(path ...string) ([]byte, error) {
	f, e := r.Open(r.join(path...))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(f)
}
func (r *ExtRoot) WriteAll(name string, data []byte, perm ...os.FileMode) error {
	d := filepath.Dir(name)
	if !r.IsExist(d) {
		if e := r.Mkdir(d, COMMON_DIR_MODE); e != nil {
			return e
		}
	}
	fm := COMMON_FILE_MODE
	if len(perm) > 0 {
		fm = perm[0]
	}
	f, e := r.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fm)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(data)
	return e
}

func IsExist(path string) bool {
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return false
	}
	return true
}

func FileSize(f *os.File) int64 {
	fi, e := f.Stat()
	if e != nil {
		return -1
	}
	return fi.Size()
}

func GBKReadAll(r io.Reader) ([]byte, error) {
	decoder := simplifiedchinese.GBK.NewDecoder()
	reader := transform.NewReader(r, decoder)
	return io.ReadAll(reader)
}

func PathEscapeGBK(s string) (string, error) {
	encoder := simplifiedchinese.GBK.NewEncoder()
	data, _, e := transform.Bytes(encoder, []byte(s))
	if e != nil {
		return "", e
	}
	// 对 GBK 编码的字节数组进行百分比编码
	var r strings.Builder
	for _, b := range data {
		r.WriteString(fmt.Sprintf("%%%02X", b))
	}
	return r.String(), nil
}

func readFile(filePath string, lineHandler func(scanner *bufio.Scanner) error) error {
	file, e := os.Open(filePath)
	if e != nil {
		return e
	}
	defer file.Close()
	return lineHandler(bufio.NewScanner(file))
}

func ReadFirstLine(filePath string) (string, error) {
	var firstLine string
	e := readFile(filePath, func(scanner *bufio.Scanner) error {
		if scanner.Scan() {
			firstLine = scanner.Text()
			return nil
		}
		if e := scanner.Err(); e != nil {
			return e
		}
		return fmt.Errorf("file is empty")
	})
	return firstLine, e
}

func ReadLines(filePath string, prefix int) ([]string, error) {
	lines := make([]string, 0, prefix)
	e := readFile(filePath, func(scanner *bufio.Scanner) error {
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if len(lines) >= prefix {
				break
			}
		}
		if e := scanner.Err(); e != nil {
			return e
		}
		return nil
	})
	return lines, e
}

func ReadLine(filePath string, fx func(string, int) bool) error {
	return readFile(filePath, func(scanner *bufio.Scanner) error {
		i := 0
		for scanner.Scan() {
			if !fx(scanner.Text(), i) {
				return nil
			}
			i++
		}
		if e := scanner.Err(); e != nil {
			return e
		}
		return nil
	})
}

func ShortSha1(text string) string {
	hash := sha1.Sum([]byte(text))
	ret := ""
	for i := 0; i < len(hash); i++ {
		if i%5 == 0 {
			ret += fmt.Sprintf("%02x", hash[i])
		}
	}
	return ret
}

func ContentType(name string) string {
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return ct
}

var hc *http.Client = &http.Client{
	Timeout: 10 * time.Second,
}

func HttpClient() *http.Client {
	return hc
}

func DoHttp(req *http.Request) (*http.Response, error) {
	return hc.Do(req)
}

func BodyReader(resp *http.Response) (io.ReadCloser, error) {
	reader := resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gzr, e := gzip.NewReader(resp.Body)
		if e != nil {
			return reader, e
		}
		reader = gzr
	}
	return reader, nil
}
