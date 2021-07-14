package command

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/urfave/cli/v2"

	errorpkg "github.com/peak/s5cmd/error"
	"github.com/peak/s5cmd/log"
	"github.com/peak/s5cmd/log/stat"
	"github.com/peak/s5cmd/storage"
	"github.com/peak/s5cmd/storage/url"
	"github.com/peak/s5cmd/strutil"
)

var sizeHelpTemplate = `Name:
	{{.HelpName}} - {{.Usage}}

Usage:
	{{.HelpName}} [options] argument

Options:
	{{range .VisibleFlags}}{{.}}
	{{end}}
Examples:
	1. Show disk usage of all objects in a bucket
		 > s5cmd {{.HelpName}} s3://bucket/*

	2. Show disk usage of all objects that match a wildcard, grouped by storage class
		 > s5cmd {{.HelpName}} --group s3://bucket/prefix/obj*.gz

	3. Show disk usage of all objects in a bucket but exclude the ones with ".py" extension
		 > s5cmd {{.HelpName}} --exclude "*.py" s3://bucket/*
`

var sizeCommand = &cli.Command{
	Name:               "du",
	HelpName:           "du",
	Usage:              "show object size usage",
	CustomHelpTemplate: sizeHelpTemplate,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "group",
			Aliases: []string{"g"},
			Usage:   "group sizes by storage class",
		},
		&cli.BoolFlag{
			Name:    "humanize",
			Aliases: []string{"H"},
			Usage:   "human-readable output for object sizes",
		},
		&cli.StringFlag{
			Name:  "exclude",
			Usage: "exclude objects with given pattern",
		},
	},
	Before: func(c *cli.Context) error {
		err := validateDUCommand(c)
		if err != nil {
			printError(givenCommand(c), c.Command.Name, err)
		}
		return err
	},
	Action: func(c *cli.Context) (err error) {
		defer stat.Collect(c.Command.FullName(), &err)()

		return Size{
			src:         c.Args().First(),
			op:          c.Command.Name,
			fullCommand: givenCommand(c),
			// flags
			groupByClass: c.Bool("group"),
			humanize:     c.Bool("humanize"),
			exclude:      c.String("exclude"),

			storageOpts: NewStorageOpts(c),
		}.Run(c.Context)
	},
}

// Size holds disk usage (du) operation flags and states.
type Size struct {
	src         string
	op          string
	fullCommand string

	// flags
	groupByClass bool
	humanize     bool
	exclude      string

	storageOpts storage.Options
}

// Run calculates disk usage of given source.
func (sz Size) Run(ctx context.Context) error {
	srcurl, err := url.New(sz.src)
	if err != nil {
		return err
	}

	client, err := storage.NewClient(ctx, srcurl, sz.storageOpts)
	if err != nil {
		printError(sz.fullCommand, sz.op, err)
		return err
	}

	storageTotal := map[string]sizeAndCount{}
	total := sizeAndCount{}

	var merror error

	for object := range client.List(ctx, srcurl, false) {
		if object.Type.IsDir() || errorpkg.IsCancelation(object.Err) {
			continue
		}

		if err := object.Err; err != nil {
			merror = multierror.Append(merror, err)
			printError(sz.fullCommand, sz.op, err)
			continue
		}
		if sz.exclude == "" || !strutil.RegexMatch(sz.exclude, object.URL.Path) {
			storageClass := string(object.StorageClass)
			s := storageTotal[storageClass]
			s.addObject(object)
			storageTotal[storageClass] = s

			total.addObject(object)
		}
	}

	if !sz.groupByClass {
		msg := SizeMessage{
			Source:        srcurl.String(),
			Count:         total.count,
			Size:          total.size,
			showHumanized: sz.humanize,
		}
		log.Info(msg)
		return nil
	}

	for k, v := range storageTotal {
		msg := SizeMessage{
			Source:        srcurl.String(),
			StorageClass:  k,
			Count:         v.count,
			Size:          v.size,
			showHumanized: sz.humanize,
		}
		log.Info(msg)
	}
	return merror
}

// SizeMessage is the structure for logging disk usage.
type SizeMessage struct {
	Source       string `json:"source"`
	StorageClass string `json:"storage_class,omitempty"`
	Count        int64  `json:"count"`
	Size         int64  `json:"size"`

	showHumanized bool
}

// humanize is a helper method to humanize bytes.
func (s SizeMessage) humanize() string {
	if s.showHumanized {
		return strutil.HumanizeBytes(s.Size)
	}
	return fmt.Sprintf("%d", s.Size)
}

// String returns the string representation of SizeMessage.
func (s SizeMessage) String() string {
	var storageCls string
	if s.StorageClass != "" {
		storageCls = fmt.Sprintf(" [%s]", s.StorageClass)
	}
	return fmt.Sprintf(
		"%s bytes in %d objects: %s%s",
		s.humanize(),
		s.Count,
		s.Source,
		storageCls,
	)
}

// JSON returns the JSON representation of SizeMessage.
func (s SizeMessage) JSON() string {
	return strutil.JSON(s)
}

type sizeAndCount struct {
	size  int64
	count int64
}

func (s *sizeAndCount) addObject(obj *storage.Object) {
	s.size += obj.Size
	s.count++
}

func validateDUCommand(c *cli.Context) error {
	if c.Args().Len() != 1 {
		return fmt.Errorf("expected only 1 argument")
	}
	return nil
}
