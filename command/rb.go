package command

import (
	"context"

	"github.com/urfave/cli/v2"

	"github.com/peak/s5cmd/log"
	"github.com/peak/s5cmd/log/stat"
	"github.com/peak/s5cmd/storage"
	"github.com/peak/s5cmd/storage/url"
)

var removeBucketHelpTemplate = `Name:
	{{.HelpName}} - {{.Usage}}

Usage:
	{{.HelpName}} s3://bucketname

Options:
	{{range .VisibleFlags}}{{.}}
	{{end}}
Examples:
	1. Deletes S3 bucket with given name
		 > s5cmd {{.HelpName}} s3://bucketname
`

var removeBucketCommand = &cli.Command{
	Name:               "rb",
	HelpName:           "rb",
	Usage:              "remove bucket",
	CustomHelpTemplate: removeBucketHelpTemplate,
	Before: func(c *cli.Context) error {
		err := validateMBCommand(c) // uses same validation function with make bucket command.
		if err != nil {
			printError(givenCommand(c), c.Command.Name, err)
		}
		return err
	},
	Action: func(c *cli.Context) (err error) {
		defer stat.Collect(c.Command.FullName(), &err)()

		return RemoveBucket{
			src:         c.Args().First(),
			op:          c.Command.Name,
			fullCommand: givenCommand(c),

			storageOpts: NewStorageOpts(c),
		}.Run(c.Context)
	},
}

// RemoveBucket holds bucket deletion operation flags and states.
type RemoveBucket struct {
	src         string
	op          string
	fullCommand string

	storageOpts storage.Options
}

// Run removes a bucket.
func (b RemoveBucket) Run(ctx context.Context) error {
	bucket, err := url.New(b.src)
	if err != nil {
		printError(b.fullCommand, b.op, err)
		return err
	}

	client, err := storage.NewRemoteClient(ctx, &url.URL{}, b.storageOpts)
	if err != nil {
		printError(b.fullCommand, b.op, err)
		return err
	}

	if err := client.RemoveBucket(ctx, bucket.Bucket); err != nil {
		printError(b.fullCommand, b.op, err)
		return err
	}

	msg := log.InfoMessage{
		Operation: b.op,
		Source:    bucket,
	}
	log.Info(msg)

	return nil
}
