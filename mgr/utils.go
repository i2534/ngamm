package mgr

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
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

func IsExist(path string) bool {
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return false
	}
	return true
}

func PathEscapeGBK(s string) (string, error) {
	// 将字符串转换为 GBK 编码的字节数组
	gbkEncoder := simplifiedchinese.GBK.NewEncoder()
	gbkBytes, _, err := transform.Bytes(gbkEncoder, []byte(s))
	if err != nil {
		return "", err
	}

	// 对 GBK 编码的字节数组进行百分比编码
	var escaped strings.Builder
	for _, b := range gbkBytes {
		escaped.WriteString(fmt.Sprintf("%%%02X", b))
	}

	return escaped.String(), nil
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
