package log

import (
	"fmt"
	"log"
	"os"

	"github.com/peak/s5cmd/flags"
	"github.com/peak/s5cmd/message"
)

// stdoutCh is used to synchronize writes to standard output. Multi-line
// logging is not possible if all workers print logs at the same time.
var stdoutCh = make(chan string, 10000)

var Logger *logger

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarning
	levelError
)

func Init() {
	Logger = New()
}

func (l logLevel) String() string {
	switch l {
	case levelInfo:
		return ""
	case levelError:
		return "ERROR"
	case levelWarning:
		return "WARNING"
	case levelDebug:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

func levelFromString(s string) logLevel {
	switch s {
	case "debug":
		return levelDebug
	case "info":
		return levelInfo
	case "warning":
		return levelWarning
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

type logger struct {
	donech chan struct{}
	impl   *log.Logger
	level  logLevel
}

func New() *logger {
	level := levelFromString(*flags.LogLevel)
	logger := &logger{
		donech: make(chan struct{}),
		impl:   log.New(os.Stdout, "", 0),
		level:  level,
	}
	go logger.stdout()
	return logger
}

func (l *logger) printf(level logLevel, message message.Message) {
	if level < l.level {
		return
	}

	if *flags.JSON {
		msg := message.JSON()
		stdoutCh <- msg
	} else {
		msg := fmt.Sprintf("%v %v", level, message.String())
		stdoutCh <- msg
	}
}

func (l *logger) Debug(msg message.Message) {
	l.printf(levelDebug, msg)
}

func (l *logger) Info(msg message.Message) {
	l.printf(levelInfo, msg)
}

func (l *logger) Warning(msg message.Message) {
	l.printf(levelWarning, msg)
}

func (l *logger) Error(msg message.Message) {
	l.printf(levelError, msg)
}

func (l *logger) stdout() {
	defer close(l.donech)

	for msg := range stdoutCh {
		l.impl.Println(msg)
	}
}

func (l *logger) Close() {
	close(stdoutCh)
	<-l.donech
}
