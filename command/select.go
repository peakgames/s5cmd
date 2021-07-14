package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/urfave/cli/v2"

	errorpkg "github.com/peak/s5cmd/error"
	"github.com/peak/s5cmd/log/stat"
	"github.com/peak/s5cmd/parallel"
	"github.com/peak/s5cmd/storage"
	"github.com/peak/s5cmd/storage/url"
)

var selectHelpTemplate = `Name:
	{{.HelpName}} - {{.Usage}}

Usage:
	{{.HelpName}} [options] argument

Options:
	{{range .VisibleFlags}}{{.}}
	{{end}}
Examples:
	01. Search for all JSON objects with the foo property set to 'bar' and spit them into stdout
		 > s5cmd {{.HelpName}} --compression gzip --query "SELECT * FROM S3Object s WHERE s.foo='bar'" s3://bucket/*
`

var selectCommandFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "query",
		Aliases: []string{"e"},
		Usage:   "SQL expression to use to select from the objects",
	},
	&cli.StringFlag{
		Name:  "compression",
		Usage: "input compression format",
		Value: "NONE",
	},
	&cli.StringFlag{
		Name:  "format",
		Usage: "input data format (only JSON supported for the moment)",
		Value: "JSON",
	},
}

var selectCommand = &cli.Command{
	Name:               "select",
	HelpName:           "select",
	Usage:              "run SQL queries on objects",
	Flags:              selectCommandFlags,
	CustomHelpTemplate: selectHelpTemplate,
	Before: func(c *cli.Context) error {
		err := validateSelectCommand(c)
		if err != nil {
			printError(givenCommand(c), c.Command.Name, err)
		}
		return err
	},
	Action: func(c *cli.Context) (err error) {
		defer stat.Collect(c.Command.FullName(), &err)()

		return Select{
			src:         c.Args().Get(0),
			op:          c.Command.Name,
			fullCommand: givenCommand(c),
			// flags
			query:           c.String("query"),
			compressionType: c.String("compression"),

			storageOpts: NewStorageOpts(c),
		}.Run(c.Context)
	},
}

// Select holds select operation flags and states.
type Select struct {
	src         string
	op          string
	fullCommand string

	query           string
	compressionType string

	// s3 options
	storageOpts storage.Options
}

// Run starts copying given source objects to destination.
func (s Select) Run(ctx context.Context) error {
	srcurl, err := url.New(s.src)
	if err != nil {
		printError(s.fullCommand, s.op, err)
		return err
	}

	client, err := storage.NewRemoteClient(ctx, srcurl, s.storageOpts)
	if err != nil {
		printError(s.fullCommand, s.op, err)
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	objch, err := expandSource(ctx, client, false, srcurl)
	if err != nil {
		printError(s.fullCommand, s.op, err)
		return err
	}

	var merror error

	waiter := parallel.NewWaiter()
	errDoneCh := make(chan bool)
	writeDoneCh := make(chan bool)
	resultCh := make(chan json.RawMessage, 128)

	go func() {
		defer close(errDoneCh)
		for err := range waiter.Err() {
			printError(s.fullCommand, s.op, err)
			merror = multierror.Append(merror, err)
		}
	}()

	go func() {
		defer close(writeDoneCh)
		var fatalError error
		for {
			record, ok := <-resultCh
			if !ok {
				break
			}
			if fatalError != nil {
				// Drain the channel.
				continue
			}
			if _, err := os.Stdout.Write(append(record, '\n')); err != nil {
				// Stop reading upstream. Notably useful for EPIPE.
				cancel()
				printError(s.fullCommand, s.op, err)
				fatalError = err
			}
		}
	}()

	for object := range objch {
		if object.Type.IsDir() || errorpkg.IsCancelation(object.Err) {
			continue
		}

		if err := object.Err; err != nil {
			printError(s.fullCommand, s.op, err)
			continue
		}

		if object.StorageClass.IsGlacier() {
			err := fmt.Errorf("object '%v' is on Glacier storage", object)
			printError(s.fullCommand, s.op, err)
			continue
		}

		task := s.prepareTask(ctx, client, object.URL, resultCh)
		parallel.Run(task, waiter)
	}

	waiter.Wait()
	<-errDoneCh
	<-writeDoneCh

	return merror
}

func (s Select) prepareTask(ctx context.Context, client *storage.S3, url *url.URL, resultCh chan<- json.RawMessage) func() error {
	return func() error {
		query := &storage.SelectQuery{
			ExpressionType:  "SQL",
			Expression:      s.query,
			CompressionType: s.compressionType,
		}

		return client.Select(ctx, url, query, resultCh)
	}
}

func validateSelectCommand(c *cli.Context) error {
	if c.Args().Len() != 1 {
		return fmt.Errorf("expected source argument")
	}

	src := c.Args().Get(0)

	srcurl, err := url.New(src)
	if err != nil {
		return err
	}

	if !srcurl.IsRemote() {
		return fmt.Errorf("source must be remote")
	}

	if !strings.EqualFold(c.String("format"), "JSON") {
		return fmt.Errorf("only json supported")
	}

	return nil
}
