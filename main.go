package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/gops/agent"
	"github.com/peak/s5cmd/complete"
	"github.com/peak/s5cmd/core"
	"github.com/peak/s5cmd/flags"
	"github.com/peak/s5cmd/stats"
	"github.com/peak/s5cmd/version"
)

//go:generate go run version/cmd/generate.go
var (
	GitSummary = version.GitSummary
	GitBranch  = version.GitBranch
)

func printOps(name string, counter uint64, elapsed time.Duration, extra string) {
	if counter == 0 {
		return
	}

	secs := elapsed.Seconds()
	if secs == 0 {
		secs = 1
	}

	ops := uint64(math.Floor((float64(counter) / secs) + 0.5))
	log.Printf("# Stats: %-7s %10d %4d ops/sec%s", name, counter, ops, extra)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%v\n\n", core.UsageLine())

		fmt.Fprint(os.Stderr, "Options:\n")
		flag.PrintDefaults()

		cl := core.CommandList()
		fmt.Fprint(os.Stderr, "\nCommands:")
		fmt.Fprintf(os.Stderr, "\n    %v\n", strings.Join(cl, ", "))
		fmt.Fprintf(os.Stderr, "\nTo get help on a specific command, run \"%v <command> -h\"\n", os.Args[0])
	}

	if err := flags.Parse(); err != nil {
		log.Print(err)
		os.Exit(2)
	}

	if done, err := complete.ParseFlagsAndRun(); err != nil {
		log.Fatal("-ERR " + err.Error())
	} else if done {
		os.Exit(0)
	}

	if *flags.EnableGops || os.Getenv("S5CMD_GOPS") != "" {
		if err := agent.Listen(&agent.Options{NoShutdownCleanup: true}); err != nil {
			log.Fatal("-ERR", err)
		}
	}

	if *flags.ShowVersion {
		fmt.Printf("s5cmd version %s", GitSummary)
		if GitBranch != "" {
			fmt.Printf(" (from branch %s)", GitBranch)
		}
		fmt.Print("\n")
		os.Exit(0)
	}

	if flag.Arg(0) == "" && *flags.CommandFile == "" {
		flag.Usage()
		os.Exit(2)
	}

	cmd := strings.Join(flag.Args(), " ")
	if cmd != "" && *flags.CommandFile != "" {
		log.Fatal("-ERR Only specify -f or command, not both")
	}
	if (cmd == "" && *flags.CommandFile == "") || *flags.WorkerCount == 0 || *flags.UploadPartSize < 1 || *flags.RetryCount < 0 {
		log.Fatal("-ERR Please specify all arguments.")
	}

	var cmdMode bool
	if cmd != "" {
		cmdMode = true
	}

	startTime := time.Now()
	parentCtx, cancelFunc := context.WithCancel(context.Background())

	exitCode := -1
	exitFunc := func(code int) {
		exitCode = code
		cancelFunc()
	}

	ctx := context.WithValue(
		context.WithValue(
			parentCtx,
			core.ExitFuncKey,
			exitFunc,
		),
		core.CancelFuncKey,
		cancelFunc,
	)

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Print("# Got signal, cleaning up...")
		cancelFunc()
	}()

	s := stats.Stats{}

	core.Verbose = *flags.Verbose

	wp := core.NewWorkerManager(ctx, &s)
	if cmdMode {
		wp.RunCmd(cmd)
	} else {
		wp.Run(*flags.CommandFile)
	}

	elapsed := time.Since(startTime)

	failops := s.Get(stats.Fail)

	// if exitCode is -1 (default) and if we have at least one absolute-fail,
	// exit with code 127
	if exitCode == -1 {
		if failops > 0 {
			exitCode = 127
		} else {
			exitCode = 0
		}
	}

	if !cmdMode {
		log.Printf("# Exiting with code %d", exitCode)
	}

	if !cmdMode || *flags.PrintStats {
		s3ops := s.Get(stats.S3Op)
		fileops := s.Get(stats.FileOp)
		shellops := s.Get(stats.ShellOp)
		printOps("S3", s3ops, elapsed, "")
		printOps("File", fileops, elapsed, "")
		printOps("Shell", shellops, elapsed, "")
		printOps("Failed", failops, elapsed, "")

		printOps("Total", s3ops+fileops+shellops+failops, elapsed, fmt.Sprintf(" %v", elapsed))
	}

	os.Exit(exitCode)
}
