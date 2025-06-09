package log

import (
	"bytes"
	rl "log"
	"os"
	"strings"
	"testing"
)

func TestPrint(t *testing.T) {
	SetFlags(rl.LstdFlags | rl.Lshortfile)

	// Test basic print functions
	Println("This is a test log message.")
	Printf("Formatted log message: %d, %s", 42, "hello")
	Print("This is another log message.")
}

func TestGroupedPrint(t *testing.T) {
	SetFlags(rl.LstdFlags | rl.Lshortfile)

	// Test group-based print functions
	Group("test").Println("This is a grouped test log message.")
	Group("test").Printf("Formatted grouped log message: %d, %s", 42, "hello")
	Group("test").Print("This is another grouped log message.")
}

func TestPrintWithOutput(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(os.Stdout)

	Println("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("Expected output to contain 'test message', got: %s", buf.String())
	}
}

func TestPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Panic("This is a panic log message that will panic.")
}

func TestPanicf(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Panicf("Formatted panic message: %d, %s", 42, "goodbye")
}

func TestPanicln(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Panicln("This is a panic log message with newline.")
}

func TestGroupedPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Group("test").Panic("This is a grouped panic log message.")
}

func TestGroupedPanicf(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Group("test").Panicf("Formatted grouped panic message: %d, %s", 42, "goodbye")
}

func TestGroupedPanicln(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but didn't get one")
		}
	}()

	Group("test").Panicln("This is a grouped panic log message with newline.")
}
