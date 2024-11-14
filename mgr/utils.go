package mgr

import (
	"bufio"
	"fmt"
	"os"
)

func readFile(filePath string, lineHandler func(scanner *bufio.Scanner) error) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	return lineHandler(scanner)
}

func ReadFirstLine(filePath string) (string, error) {
	var firstLine string
	err := readFile(filePath, func(scanner *bufio.Scanner) error {
		if scanner.Scan() {
			firstLine = scanner.Text()
			return nil
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("file is empty")
	})
	return firstLine, err
}

func ReadLines(filePath string, prefix int) ([]string, error) {
	lines := make([]string, 0, prefix)
	err := readFile(filePath, func(scanner *bufio.Scanner) error {
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if len(lines) >= prefix {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return nil
	})
	return lines, err
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
		if err := scanner.Err(); err != nil {
			return err
		}
		return nil
	})
}
