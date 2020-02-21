package core

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/peak/s5cmd/objurl"
	"github.com/peak/s5cmd/op"
	"github.com/peak/s5cmd/opt"
	"github.com/peak/s5cmd/storage"
)

func S3BatchDownload(command *Command, object *storage.Object) *Job {
	cmd := "cp"
	if command.operation == op.AliasBatchGet {
		cmd = "get"
	}

	if command.opts.Has(opt.DeleteSource) {
		cmd = "mv"
	}

	cmd += command.opts.GetParams()
	dst := command.dst

	var dstFn string
	if command.opts.Has(opt.Parents) {
		dstFn = object.URL.Path
	} else {
		dstFn = object.URL.Base()
	}

	joinfn := filepath.Join
	if dst.IsRemote() {
		joinfn = path.Join
	}

	clone := dst.Clone()
	clone.Path = joinfn(clone.Path, dstFn)
	dir := filepath.Dir(clone.Absolute())
	os.MkdirAll(dir, os.ModePerm)
	return command.makeJob(cmd, op.Download, object.URL, clone)
}

func S3BatchCopy(command *Command, object *storage.Object) *Job {
	cmd := "cp"
	if command.opts.Has(opt.DeleteSource) {
		cmd = "mv"
	}
	cmd += command.opts.GetParams()

	dst := command.dst
	src := object.URL

	var dstFn string
	if command.opts.Has(opt.Parents) {
		dstFn = object.URL.Path
	} else {
		dstFn = object.URL.Base()
	}

	dstPath := fmt.Sprintf("s3://%v/%v%v", dst.Bucket, dst.Path, dstFn)
	dstUrl, _ := objurl.New(dstPath)
	return command.makeJob(cmd, op.Copy, src, dstUrl)
}
