package command

import (
	"github.com/peak/s5cmd/log/stat"
	"github.com/peak/s5cmd/storage"

	"github.com/urfave/cli/v2"
)

var moveHelpTemplate = `Name:
	{{.HelpName}} - {{.Usage}}

Usage:
	{{.HelpName}} [options] source destination

Options:
	{{range .VisibleFlags}}{{.}}
	{{end}}
Examples:
	1. Move an S3 object to working directory
		 > s5cmd {{.HelpName}} s3://bucket/prefix/object.gz .

	2. Move an S3 object and rename
		 > s5cmd {{.HelpName}} s3://bucket/prefix/object.gz myobject.gz

	3. Move all S3 objects to a directory
		 > s5cmd {{.HelpName}} s3://bucket/* target-directory/

	4. Move a file to S3 bucket
		 > s5cmd {{.HelpName}} myfile.gz s3://bucket/

	5. Move a directory to S3 bucket recursively
		 > s5cmd {{.HelpName}} dir/ s3://bucket/
`

var moveCommand = &cli.Command{
	Name:               "mv",
	HelpName:           "mv",
	Usage:              "move/rename objects",
	Flags:              copyCommandFlags, // move and copy commands share the same flags
	CustomHelpTemplate: moveHelpTemplate,
	Before: func(c *cli.Context) error {
		return copyCommand.Before(c)
	},
	Action: func(c *cli.Context) (err error) {
		defer stat.Collect(c.Command.FullName(), &err)()

		copyCommand := Copy{
			src:          c.Args().Get(0),
			dst:          c.Args().Get(1),
			op:           c.Command.Name,
			fullCommand:  givenCommand(c),
			deleteSource: true, // delete source
			// flags
			noClobber:        c.Bool("no-clobber"),
			ifSizeDiffer:     c.Bool("if-size-differ"),
			ifSourceNewer:    c.Bool("if-source-newer"),
			flatten:          c.Bool("flatten"),
			followSymlinks:   !c.Bool("no-follow-symlinks"),
			storageClass:     storage.StorageClass(c.String("storage-class")),
			encryptionMethod: c.String("sse"),
			encryptionKeyID:  c.String("sse-kms-key-id"),
			acl:              c.String("acl"),
			cacheControl:     c.String("cache-control"),
			expires:          c.String("expires"),


			storageOpts: NewStorageOpts(c),
		}

		return copyCommand.Run(c.Context)
	},
}
