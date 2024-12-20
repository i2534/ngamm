package mgr

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

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
