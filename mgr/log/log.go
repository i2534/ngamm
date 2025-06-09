package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"
)

const (
	GROUP_ANY   string = "*"
	GROUP_TOPIC string = "topic"
	GROUP_NGA   string = "nga"
	GROUP_PAN   string = "pan"
	GROUP_GIN   string = "gin"
)

type Groups []string

func (g Groups) IsLog(group string) bool {
	if len(g) == 0 || group == GROUP_ANY {
		return true
	}
	return slices.Contains(g, strings.ToLower(group))
}

type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
	Println(v ...any)

	Fatal(v ...any)
	Fatalf(format string, v ...any)
	Fatalln(v ...any)

	Panic(v ...any)
	Panicf(format string, v ...any)
	Panicln(v ...any)
}

type logger struct {
	*log.Logger
	gs Groups
}

type nopLogger struct{}

var (
	gsa = make(Groups, 0)
	std = &logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
		gs:     gsa,
	}
	nop = &nopLogger{}
)

func SetFlags(flags int) {
	std.SetFlags(flags)
}
func SetOutput(w io.Writer) {
	std.SetOutput(w)
}
func SetGroups(gs Groups) {
	if len(gs) > 0 {
		if slices.Contains(gs, GROUP_ANY) || slices.Contains(gs, "all") {
			std.gs = gsa
		} else {
			std.gs = gs
		}
	}
}
func IsLog(group string) bool {
	if group == "" {
		group = GROUP_ANY
	}
	return std.gs.IsLog(group)
}

func Group(group string) Logger {
	if group == "" {
		group = GROUP_ANY
	}
	if std.gs.IsLog(group) {
		return std
	} else {
		return nop
	}
}

func Println(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprintln(v...))
	}
}
func Printf(format string, v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprintf(format, v...))
	}
}
func Print(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprint(v...))
	}
}

func Fatal(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprint(v...))
		os.Exit(1)
	}
}

func Fatalf(format string, v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprintf(format, v...))
		os.Exit(1)
	}
}

func Fatalln(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprintln(v...))
		os.Exit(1)
	}
}

func Panic(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		std.Output(2, fmt.Sprint(v...))
		panic(fmt.Sprint(v...))
	}
}

func Panicf(format string, v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		msg := fmt.Sprintf(format, v...)
		std.Output(2, msg)
		panic(msg)
	}
}

func Panicln(v ...any) {
	if std.gs.IsLog(GROUP_ANY) {
		msg := fmt.Sprintln(v...)
		std.Output(2, msg)
		panic(msg)
	}
}

func (l *nopLogger) Print(v ...any) {
}
func (l *nopLogger) Printf(format string, v ...any) {
}
func (l *nopLogger) Println(v ...any) {
}
func (l *nopLogger) Fatal(v ...any) {
}
func (l *nopLogger) Fatalf(format string, v ...any) {
}
func (l *nopLogger) Fatalln(v ...any) {
}
func (l *nopLogger) Panic(v ...any) {
}
func (l *nopLogger) Panicf(format string, v ...any) {
}
func (l *nopLogger) Panicln(v ...any) {
}
